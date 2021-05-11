// +build linux

package server

import (
	"context"
	"testing"

	"github.com/cri-o/cri-o/pkg/annotations"
	"github.com/cri-o/cri-o/server/cri/types"
	"github.com/opencontainers/runtime-tools/generate"
)

func TestAddOCIBindsForDev(t *testing.T) {
	specgen, err := generate.New("linux")
	if err != nil {
		t.Error(err)
	}
	config := &types.ContainerConfig{
		Mounts: []*types.Mount{
			{
				ContainerPath: "/dev",
				HostPath:      "/dev",
			},
		},
	}
	_, binds, err := addOCIBindMounts(context.Background(), "", config, &specgen, "", nil)
	if err != nil {
		t.Error(err)
	}
	for _, m := range specgen.Mounts() {
		if m.Destination == "/dev" {
			t.Error("/dev shouldn't be in the spec if it's bind mounted from kube")
		}
	}
	var foundDev bool
	for _, b := range binds {
		if b.Destination == "/dev" {
			foundDev = true
			break
		}
	}
	if !foundDev {
		t.Error("no /dev mount found in spec mounts")
	}
}

func TestAddOCIBindsForSys(t *testing.T) {
	specgen, err := generate.New("linux")
	if err != nil {
		t.Error(err)
	}
	config := &types.ContainerConfig{
		Mounts: []*types.Mount{
			{
				ContainerPath: "/sys",
				HostPath:      "/sys",
			},
		},
	}
	_, binds, err := addOCIBindMounts(context.Background(), "", config, &specgen, "", nil)
	if err != nil {
		t.Error(err)
	}
	var howManySys int
	for _, b := range binds {
		if b.Destination == "/sys" && b.Type != "sysfs" {
			howManySys++
		}
	}
	if howManySys != 1 {
		t.Error("there is not a single /sys bind mount")
	}
}

func TestRdtClassFromAnnotations(t *testing.T) {
	containerName := "test-container"
	containerAnnotations := map[string]string{annotations.RdtContainerAnnotation: "class-1"}
	podAnnotations := map[string]string{
		annotations.RdtPodAnnotationContainerPrefix + containerName: "class-2",
		annotations.RdtPodAnnotation:                                "class-3"}

	cls, _ := rdtClassFromAnnotations(containerName, containerAnnotations, podAnnotations)
	if cls != "class-1" {
		t.Errorf("invalid rdt class, expecting \"class-1\", got %q", cls)
	}

	delete(containerAnnotations, annotations.RdtContainerAnnotation)
	cls, _ = rdtClassFromAnnotations(containerName, containerAnnotations, podAnnotations)
	if cls != "class-2" {
		t.Errorf("invalid rdt class, expecting \"class-2\", got %q", cls)
	}

	delete(podAnnotations, annotations.RdtPodAnnotationContainerPrefix+containerName)
	cls, _ = rdtClassFromAnnotations(containerName, containerAnnotations, podAnnotations)
	if cls != "class-3" {
		t.Errorf("invalid rdt class, expecting \"class-3\", got %q", cls)
	}

	delete(podAnnotations, annotations.RdtPodAnnotation)
	cls, ok := rdtClassFromAnnotations(containerName, containerAnnotations, podAnnotations)
	if ok {
		t.Errorf("unexpected rdt class for container: %q", cls)
	}

}
