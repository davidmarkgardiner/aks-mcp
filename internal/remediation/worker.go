package remediation

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// PodDeleter makes the restricted worker testable without a cluster.
type PodDeleter interface {
	DeletePod(context.Context, string, string) error
}

type PodDeleterFunc func(context.Context, string, string) error

func (f PodDeleterFunc) DeletePod(ctx context.Context, namespace, pod string) error {
	return f(ctx, namespace, pod)
}

type InClusterPodDeleter struct{ client kubernetes.Interface }

func NewInClusterPodDeleter() (*InClusterPodDeleter, error) {
	configuration, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster worker identity: %w", err)
	}
	client, err := kubernetes.NewForConfig(configuration)
	if err != nil {
		return nil, fmt.Errorf("create in-cluster worker client: %w", err)
	}
	return &InClusterPodDeleter{client: client}, nil
}

func (d *InClusterPodDeleter) DeletePod(ctx context.Context, namespace, pod string) error {
	return d.client.CoreV1().Pods(namespace).Delete(ctx, pod, metav1.DeleteOptions{})
}

// KubernetesApplier is used only by the separately deployed remediation
// worker. It uses the in-cluster service account rather than a kubeconfig or a
// shell command and supports the one typed plan action currently admitted.
type KubernetesApplier struct{ Deleter PodDeleter }

func (a KubernetesApplier) Apply(ctx context.Context, plan Plan) error {
	if a.Deleter == nil {
		return fmt.Errorf("restricted worker has no Kubernetes client")
	}
	if plan.Action != RestartFailedPod {
		return fmt.Errorf("unsupported remediation action %q", plan.Action)
	}
	pod, ok := strings.CutPrefix(plan.Scope.Resource, "pod/")
	if !ok || !safeKubernetesName(pod) || !safeKubernetesName(plan.Scope.Namespace) {
		return fmt.Errorf("invalid approved pod scope")
	}
	if err := a.Deleter.DeletePod(ctx, plan.Scope.Namespace, pod); err != nil {
		return fmt.Errorf("approved pod restart failed")
	}
	return nil
}

func safeKubernetesName(value string) bool {
	if value == "" || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}
