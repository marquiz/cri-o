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
func (s *Server) getPodQoSResourcesInfo() []*types.QOSResourceInfo {
	return []*types.QOSResourceInfo{}
}

// getContainerQoSResourcesInfo returns information about all container-level QoS resources.
func (s *Server) getContainerQoSResourcesInfo() []*types.QOSResourceInfo {
	info := []*types.QOSResourceInfo{}

	// RDT
	if rdtClasses := s.Config().Rdt().GetClasses(); len(rdtClasses) > 0 {
		info = append(info,
			&types.QOSResourceInfo{
				Name:    types.QoSResourceRdt,
				Mutable: false,
				Classes: createClassInfos(rdtClasses...),
			})
	}

	// blockio
	if blockioClasses := s.Config().BlockIO().GetClasses(); len(blockioClasses) > 0 {
		info = append(info,
			&types.QOSResourceInfo{
				Name:    types.QoSResourceBlockio,
				Mutable: false,
				Classes: createClassInfos(blockioClasses...),
			})
	}

	return info
}

func createClassInfos(names ...string) []*types.QOSResourceClassInfo {
	out := make([]*types.QOSResourceClassInfo, len(names))
	for i, name := range names {
		out[i] = &types.QOSResourceClassInfo{Name: name}
	}
	return out
}

// handleSandboxQoSResources handles QoS resource requests for a pod sandbox.
func (s *Server) handleSandboxQoSResources(config *types.PodSandboxConfig) error {
	for _, r := range config.GetQOSResources() {
		n := r.GetName()
		c := r.GetClass()
		switch n {
		default:
			return fmt.Errorf("unknown QoS resource type %q", n)
		}

		if c == "" {
			return fmt.Errorf("empty class name not allowed for QoS resource type %q", n)
		}
	}
	return nil
}

// handleContainerQoSResources handles QoS resource requests for a container.
func (s *Server) handleContainerQoSResources(spec *rspec.Spec, container *types.ContainerConfig, sb *sandbox.Sandbox) error {
	// Handle QoS resource assignments
	for _, r := range container.GetQOSResources() {
		n := r.GetName()
		c := r.GetClass()
		switch n {
		case types.QoSResourceRdt:
		case types.QoSResourceBlockio:
			// We handle RDT and BlockIO separately in as we have pod and
			// container annotations as fallback interface and it isn't enough
			// to rely on the QoS resources in CRI only
		default:
			return fmt.Errorf("unknown QoS resource type %q", n)
		}

		if c == "" {
			return fmt.Errorf("empty class name not allowed for QoS resource type %q", n)
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
		if s.Config().BlockIO().ReloadRequired() {
			if err := s.Config().BlockIO().Reload(); err != nil {
				logrus.Warnf("Reconfiguring blockio for container %s failed: %v", container.Metadata.Name, err)
			}
		}
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
	for _, r := range container.GetQOSResources() {
		if r.GetName() == resourceType {
			cls := r.GetClass()
			logrus.Debugf("%s class %q from container config (%s)", resourceType, cls, containerName)
			return cls, true
		}
	}
	return "", false
}
