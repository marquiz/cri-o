package server

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/containers/nri-plugins/pkg/sysfs"
	"github.com/cri-o/cri-o/internal/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/resource"
	types "k8s.io/cri-api/pkg/apis/runtime/v1"
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

func newZone(name, typ string, id int, parent *types.ResourceTopologyZone) *types.ResourceTopologyZone {
	idStr := strconv.Itoa(id)
	return &types.ResourceTopologyZone{
		Type:       typ,
		Name:       name,
		Parent:     parent.Name,
		Attributes: map[string]string{"id": idStr},
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

	root := &types.ResourceTopologyZone{
		Name:       "System",
		Attributes: getSystemAttributes(ctx),
	}

	if swap, err := getSwapCapacity(ctx); err != nil {
		log.Errorf(ctx, "Failed to determine swap capacity: %v", err)
	} else {
		// NOTE: non-standard resource name
		root.Resources = append(root.Resources, &types.ResourceTopologyResourceInfo{
			Name:     "swap",
			Capacity: &swap,
		})
	}

	zones := map[string]*types.ResourceTopologyZone{
		root.Name: root,
	}

	addZone := func(name, typ string, id int, parent *types.ResourceTopologyZone) *types.ResourceTopologyZone {
		zone := newZone(name, typ, id, parent)
		if _, ok := zones[zone.Name]; ok {
			log.Infof(ctx, "zone %s already exists", zone.Name)
			return zones[zone.Name]
		}
		zones[zone.Name] = zone
		return zone
	}

	//
	// CPU tree
	//
	for _, packageID := range sys.PackageIDs() {
		pkg := sys.Package(packageID)
		packageZone := addZone(
			fmt.Sprintf("Package-%d", packageID),
			types.ResourceTopologyZonePackage,
			packageID,
			root)

		// Loop over cores of the package
		threadsPerCore := map[int][]string{}
		for _, cpuID := range pkg.CPUSet().List() {
			cpu := sys.CPU(cpuID)
			if !cpu.Online() {
				continue
			}

			coreID := cpu.CoreID()
			threadsPerCore[coreID] = append(threadsPerCore[coreID], strconv.Itoa(cpuID))
		}

		for coreID, cpuIDs := range threadsPerCore {
			coreZone := addZone(
				fmt.Sprintf("Core-%d.%d", packageID, coreID),
				types.ResourceTopologyZoneCore,
				coreID,
				packageZone)
			coreZone.Resources = []*types.ResourceTopologyResourceInfo{
				{
					Name:     "cpu",
					Capacity: resource.NewQuantity(int64(len(cpuIDs)), resource.DecimalSI),
				},
			}
			coreZone.Attributes["cpu-ids"] = strings.Join(cpuIDs, ",")
		}
	}

	//
	// Memory tree
	//
	nodeIDs := sys.NodeIDs()
	for _, nodeID := range nodeIDs {
		node := sys.Node(nodeID)
		nodeZone := addZone(
			fmt.Sprintf("Node-%d", nodeID),
			types.ResourceTopologyZoneNUMANode,
			nodeID,
			root)

		// Get memory info for NUMA node
		// TODO: hugepages
		memoryInfo, err := node.MemoryInfo()
		if err != nil {
			log.Errorf(ctx, "Error fetching node memory info: %v", err)
			panic(err)
		}
		nodeZone.Resources = []*types.ResourceTopologyResourceInfo{
			{
				Name:     "memory",
				Capacity: resource.NewQuantity(int64(memoryInfo.MemTotal), resource.BinarySI),
			},
		}
		nodeZone.Attributes["cpu-ids"] = node.CPUSet().String()

		Costs := make([]*types.ResourceTopologyCost, 0, len(nodeIDs))
		for i, distance := range node.Distance() {
			Costs = append(Costs, &types.ResourceTopologyCost{
				Name:  fmt.Sprintf("Node-%d", nodeIDs[i]),
				Value: uint32(distance),
			})
		}
		nodeZone.Costs = Costs
	}

	// Create a sorted slice of zones
	zoneList := make([]*types.ResourceTopologyZone, 0, len(zones))
	for _, zone := range zones {
		log.Infof(ctx, "zone: %s resources: %v attributes %v", zone.Name, zone.Resources, zone.Attributes)
		zoneList = append(zoneList, zone)
	}
	sort.Slice(zoneList, func(i, j int) bool {
		return zoneList[i].Name < zoneList[j].Name
	})

	dynamicResourceConfig := types.DynamicRuntimeConfigResponse{
		ResourceTopology: &types.ResourceTopology{
			Zones: zoneList,
		},
	}
	s.DynamicRuntimeConfigChan <- dynamicResourceConfig
}

func getSystemAttributes(ctx context.Context) map[string]string {
	attrs := map[string]string{}
	for name, path := range map[string]string{
		types.ResourceTopologyAttributeMachineID:  "/etc/machine-id",
		types.ResourceTopologyAttributeBootID:     "/proc/sys/kernel/random/boot_id",
		types.ResourceTopologyAttributeSystemUUID: "/sys/class/dmi/id/product_uuid",
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
