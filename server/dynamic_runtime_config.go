package server

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/containers/nri-plugins/pkg/sysfs"
	"github.com/cri-o/cri-o/internal/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/resource"
	types "k8s.io/cri-api/pkg/apis/runtime/v1"
	"sigs.k8s.io/yaml"
)

type dynamicRuntimeConfigConn struct {
	stop chan struct{}
	err  error
}

// GetDynamicRuntimeConfig gets runtime configurations from the CRI runtime
func (s *Server) GetDynamicRuntimeConfig(_ *types.DynamicRuntimeConfigRequest, runtimeConfigServer types.RuntimeService_GetDynamicRuntimeConfigServer) error {
	s.dynamicRuntimeConfigBroadcaster.Do(func() {
		go s.broadcastRuntimeConfig()
	})
	conn := &dynamicRuntimeConfigConn{
		stop: make(chan struct{}),
	}

	log.Infof(context.Background(), "GetDynamicRuntimeConfig called")
	s.dynamicRuntimeConfigClients.Store(runtimeConfigServer, conn)

	// Send info when client connects
	// TODO: maybe refactor the code and do caching of info
	s.fetchMachineInfo(context.Background())

	// wait here until we don't want to send events to this client anymore
	<-conn.stop
	s.dynamicRuntimeConfigClients.Delete(runtimeConfigServer)
	return conn.err
}

func (s *Server) broadcastRuntimeConfig() {
	// notify all connections that dynamicRuntimeConfigClients has been closed
	defer s.dynamicRuntimeConfigClients.Range(func(_, value any) bool { // nolint: unparam
		if conn, ok := value.(*dynamicRuntimeConfigConn); ok {
			close(conn.stop)
		}
		return true
	})

	for runtimeConfig := range s.DynamicRuntimeConfigChan {
		s.dynamicRuntimeConfigClients.Range(func(key, value any) bool {
			stream, ok := key.(types.RuntimeService_GetDynamicRuntimeConfigServer)
			if !ok {
				return true
			}
			conn, ok := value.(*dynamicRuntimeConfigConn)
			if !ok {
				return true
			}

			if err := stream.Send(&runtimeConfig); err != nil {
				code, _ := status.FromError(err)
				// when the client closes the connection this error is expected
				// so only log non transport closing errors
				if code.Code() != codes.Unavailable && code.Message() != "transport is closing" {
					conn.err = err
				}
				// notify our waiting client connection that we are done
				close(conn.stop)
			}
			return true
		})
	}
}

func (s *Server) machineInfoUpdater(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 30)
	for ; ; <-ticker.C {
		s.fetchMachineInfo(ctx)
	}
}

func (s *Server) fetchMachineInfo(ctx context.Context) {
	fmt.Println("Fetching the  machine info...")
	sys, e := sysfs.DiscoverSystem()
	if e != nil {
		panic(e)
	}

	res := types.ResourceTopology{}

	//
	// Swap
	//
	if swap, err := getSwapCapacity(ctx); err != nil {
		log.Errorf(ctx, "Failed to determine swap capacity: %v", err)
	} else {
		res.SwapInfo = &types.ResourceSwapInfo{
			Capacity: &swap,
		}
	}

	//
	// CPUs
	//
	for _, cpuID := range sys.CPUIDs() {
		cpu := sys.CPU(cpuID)
		res.CpuInfo = append(res.CpuInfo, &types.ResourceCpuInfo{
			Id:       int64(cpuID),
			CoreId:   int64(cpu.CoreID()),
			SocketId: int64(cpu.PackageID()),
		})
	}

	//
	// NUMA nodes
	//
	for _, nodeID := range sys.NodeIDs() {
		node := sys.Node(nodeID)
		memoryInfo, err := node.MemoryInfo()
		if err != nil {
			log.Errorf(ctx, "Error fetching node memory info: %v", err)
			panic(err)
		}

		c := node.CPUSet().List()
		cpuIds := make([]int64, len(c))
		for i, v := range c {
			cpuIds[i] = int64(v)
		}

		d := node.Distance()
		distance := make([]int64, len(d))
		for i, v := range d {
			distance[i] = int64(v)
		}

		res.NumaNodeInfo = append(res.NumaNodeInfo, &types.ResourceNumaNodeInfo{
			Id: int64(nodeID),
			Resources: []*types.ResourceTopologyResourceInfo{
				{
					Name:     "memory",
					Capacity: resource.NewQuantity(int64(memoryInfo.MemTotal), resource.BinarySI),
				}},
			Distance: distance,
			CpuIds:   cpuIds,
		})
	}

	rsp := types.DynamicRuntimeConfigResponse{
		ResourceTopology: &res,
		SystemAttributes: getSystemAttributes(ctx),
	}
	str, _ := yaml.Marshal(rsp)
	log.Infof(ctx, "Sending machine info: %s", str)
	s.DynamicRuntimeConfigChan <- rsp
}

func getSystemAttributes(ctx context.Context) map[string]string {
	attrs := map[string]string{}
	for name, path := range map[string]string{
		"machine-id":  "/etc/machine-id",
		"boot-id":     "/proc/sys/kernel/random/boot_id",
		"system-uuid": "/sys/class/dmi/id/product_uuid",
	} {
		value, err := os.ReadFile(path)
		if err != nil {
			log.Errorf(ctx, "Error reading %q: %v", name, err)
		}
		attrs[name] = strings.TrimSpace(string(value))
	}
	return attrs
}

func getSwapCapacity(ctx context.Context) (resource.Quantity, error) {
	out, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return resource.Quantity{}, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "SwapTotal:") {
			fields := strings.Fields(line)
			if len(fields) != 3 {
				return resource.Quantity{}, fmt.Errorf("unexpected number of fields in SwapTotal line: %q", line)
			}
			swapCapacity, err := resource.ParseQuantity(fields[1] + "Ki")
			if err != nil {
				return resource.Quantity{}, err
			}
			return swapCapacity, nil
		}
	}

	return resource.Quantity{}, fmt.Errorf("SwapTotal line not found in /proc/meminfo")
}
