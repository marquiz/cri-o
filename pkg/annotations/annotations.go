package annotations

const (
	// UsernsMode is the user namespace mode to use
	UsernsModeAnnotation = "io.kubernetes.cri-o.userns-mode"

	// UnifiedCgroupAnnotation specifies the unified configuration for cgroup v2
	UnifiedCgroupAnnotation = "io.kubernetes.cri-o.UnifiedCgroup"

	// SpoofedContainer indicates a container was spoofed in the runtime
	SpoofedContainer = "io.kubernetes.cri-o.Spoofed"

	// ShmSizeAnnotation is the K8S annotation used to set custom shm size
	ShmSizeAnnotation = "io.kubernetes.cri-o.ShmSize"

	// DevicesAnnotation is a set of devices to give to the container
	DevicesAnnotation = "io.kubernetes.cri-o.Devices"

	// CPULoadBalancingAnnotation indicates that load balancing should be disabled for CPUs used by the container
	CPULoadBalancingAnnotation = "cpu-load-balancing.crio.io"

	// CPUQuotaAnnotation indicates that CPU quota should be disabled for CPUs used by the container
	CPUQuotaAnnotation = "cpu-quota.crio.io"

	// IRQLoadBalancingAnnotation indicates that IRQ load balancing should be disabled for CPUs used by the container
	IRQLoadBalancingAnnotation = "irq-load-balancing.crio.io"

	// OCISeccompBPFHookAnnotation is the annotation used by the OCI seccomp BPF hook for tracing container syscalls
	OCISeccompBPFHookAnnotation = "io.containers.trace-syscall"

	// RdtContainerAnnotation is the CRI level container annotation for setting
	// the RDT class (CLOS) of a container
	RdtContainerAnnotation = "io.kubernetes.cri.rdt-class"

	// RdtPodAnnotation is a Pod annotation for setting the RDT class (CLOS) of
	// all containers of the pod
	RdtPodAnnotation = "rdt.resources.beta.kubernetes.io/pod"

	// RdtPodAnnotationContainerPrefix is prefix for per-container Pod annotation
	// for setting the RDT class (CLOS) of one container of the pod
	RdtPodAnnotationContainerPrefix = "rdt.resources.beta.kubernetes.io/container."
)

var AllAllowedAnnotations = []string{
	UsernsModeAnnotation,
	UnifiedCgroupAnnotation,
	ShmSizeAnnotation,
	DevicesAnnotation,
	CPULoadBalancingAnnotation,
	CPUQuotaAnnotation,
	IRQLoadBalancingAnnotation,
	OCISeccompBPFHookAnnotation,
	RdtContainerAnnotation,
}
