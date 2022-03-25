package server

import (
	"fmt"

	"github.com/cri-o/cri-o/internal/config/rdt"
	"github.com/cri-o/cri-o/internal/lib/sandbox"
	"github.com/intel/goresctrl/pkg/blockio"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	types "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// getPodQoSResourcesInfo returns information about all container-level QoS resources.
func (s *Server) getPodQoSResourcesInfo() []*types.QoSResourceInfo {
	return []*types.QoSResourceInfo{}
}

// getContainerQoSResourcesInfo returns information about all container-level QoS resources.
func (s *Server) getContainerQoSResourcesInfo() []*types.QoSResourceInfo {
	info := []*types.QoSResourceInfo{}

	// RDT
	if rdtClasses := s.Config().Rdt().GetClasses(); len(rdtClasses) > 0 {
		info = append(info,
			&types.QoSResourceInfo{
				Name:    types.QoSResourceRdt,
				Mutable: false,
				Classes: createClassInfos(rdtClasses...),
			})
	}

	// blockio
	if blockioClasses := s.Config().BlockIO().GetClasses(); len(blockioClasses) > 0 {
		info = append(info,
			&types.QoSResourceInfo{
				Name:    types.QoSResourceBlockio,
				Mutable: false,
				Classes: createClassInfos(blockioClasses...),
			})
	}

	return info
}

func createClassInfos(names ...string) []*types.QoSResourceClassInfo {
	out := make([]*types.QoSResourceClassInfo, len(names))
	for i, name := range names {
		out[i] = &types.QoSResourceClassInfo{Name: name}
	}
	return out
}

// handleSandboxQoSResources handles QoS resource requests for a pod sandbox.
func (s *Server) handleSandboxQoSResources(config *types.PodSandboxConfig) error {
	for r, c := range config.GetQosResources().GetClasses() {
		switch r {
		default:
			return fmt.Errorf("unknown QoS resource type %q", r)
		}

		if c == "" {
			return fmt.Errorf("empty class name not allowed for QoS resource type %q", r)
		}
	}
	return nil
}

// handleContainerQoSResources handles QoS resource requests for a container.
func (s *Server) handleContainerQoSResources(spec *rspec.Spec, container *types.ContainerConfig, sb *sandbox.Sandbox) error {
	// Handle QoS resource assignments
	for r, c := range container.GetQosResources().GetClasses() {
		switch r {
		case types.QoSResourceRdt:
		case types.QoSResourceBlockio:
			// We handle RDT and BlockIO separately in as we have pod and
			// container annotations as fallback interface and it isn't enough
			// to rely on the QoS resources in CRI only
		default:
			return fmt.Errorf("unknown QoS resource type %q", r)
		}

		if c == "" {
			return fmt.Errorf("empty class name not allowed for QoS resource type %q", r)
		}
	}

	// Handle RDT
	rdtClass, err := s.getContainerRdtClass(container, sb)
	if err != nil {
		return err
	}
	if rdtClass != "" {
		logrus.Debugf("Setting RDT ClosID of container %s to %q", container.Metadata.Name, rdt.ResctrlPrefix+rdtClass)
		// TODO: patch runtime-tools to support setting ClosID via a helper func similar to SetLinuxIntelRdtL3CacheSchema()
		spec.Linux.IntelRdt = &rspec.LinuxIntelRdt{ClosID: rdt.ResctrlPrefix + rdtClass}
	}

	// Handle BlockIO
	blockioClass, err := s.getContainerBlockioClass(container, sb)
	if err != nil {
		return err
	}
	if blockioClass != "" {
		if linuxBlockIO, err := blockio.OciLinuxBlockIO(blockioClass); err == nil {
			if spec.Linux.Resources == nil {
				spec.Linux.Resources = &rspec.LinuxResources{}
			}
			spec.Linux.Resources.BlockIO = linuxBlockIO
		}
	}

	return nil
}

// getContainerRdtClass gets the effective RDT class of a container.
func (s *Server) getContainerRdtClass(container *types.ContainerConfig, sb *sandbox.Sandbox) (string, error) {
	crioRdt := s.Config().Rdt()
	containerName := container.Metadata.Name

	cls, ok := getClassFromResourceConfig(types.QoSResourceRdt, container, sb)

	// If class is not specified in CRI QoS resources we check the annotations
	if !ok {
		var err error
		cls, err = crioRdt.ContainerClassFromAnnotations(containerName, container.Annotations, sb.Annotations())
		if err != nil {
			return "", err
		}
		if cls != "" {
			logrus.Debugf("RDT class %q from annotations (%s)", cls, ok, containerName)
		}
	}

	if cls != "" && !crioRdt.Enabled() {
		return "", fmt.Errorf("RDT disabled, refusing to set RDT class of container %q to %q", containerName, cls)
	}

	return cls, nil
}

// getContainerBlockioClass gets the effective BlockIO class of a container.
func (s *Server) getContainerBlockioClass(container *types.ContainerConfig, sb *sandbox.Sandbox) (string, error) {
	crioBlockio := s.Config().BlockIO()
	containerName := container.Metadata.Name

	cls, ok := getClassFromResourceConfig(types.QoSResourceBlockio, container, sb)

	// If class is not specified in CRI QoS resources we check the annotations
	if !ok {
		var err error
		cls, err = blockio.ContainerClassFromAnnotations(containerName, container.Annotations, sb.Annotations())
		if err != nil {
			return "", err
		}
		if cls != "" {
			logrus.Debugf("BlockIO class %q from annotations (%s)", cls, ok, containerName)
		}
	}

	if cls != "" && !crioBlockio.Enabled() {
		return "", fmt.Errorf("BlockIO disabled, refusing to set blockio class of container %q to %q", containerName, cls)
	}

	return cls, nil
}

func getClassFromResourceConfig(resourceType string, container *types.ContainerConfig, sb *sandbox.Sandbox) (string, bool) {
	// Get class from container resources
	cls, ok := container.GetQosResources().GetClasses()[resourceType]
	if cls != "" && ok {
		logrus.Debugf("%s class %q (%s) from container config (%s)", resourceType, cls, ok, containerName)
	}
	return cls, ok
}
