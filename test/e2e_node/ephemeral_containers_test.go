package e2enode

import (
    "context"
    "fmt"
    "io/ioutil"
    "path/filepath"
    "time"

    "github.com/onsi/ginkgo"
    "github.com/onsi/gomega"

    "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/klog"
    "k8s.io/kubernetes/pkg/features"
    kubeletconfig "k8s.io/kubernetes/pkg/kubelet/apis/config"
    "k8s.io/kubernetes/test/e2e/framework"
)

func createStaticPodWithEphemeralContainer(f *framework.Framework) error {
    template := `
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  containers:
  - name: test-container
    image: %s
    command: ["sh", "-c", "sleep 3600"]
  ephemeralContainers:
  - name: ephemeral-container
    image: %s
    imagePullPolicy: Never
    tty: true
    stdin: true
    terminationMessagePolicy: FallbackToLogsOnError
    command: ["sh", "-c", "sleep 3600"]		
`
    return ioutil.WriteFile(filepath.Join(framework.TestContext.KubeletConfig.StaticPodPath, "test-static-pod.yaml"), []byte(fmt.Sprintf(template, "test-static-pod", f.Namespace.Name, busyboxImage, busyboxImage)), 0644)
}

func createPodWithoutEphemeralContainer(f *framework.Framework) *v1.Pod {
    podDesc := &v1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Name: "test-pod",
        },
        Spec: v1.PodSpec{
            Containers: []v1.Container{
                {
                    Name:    "test",
                    Image:   busyboxImage,
                    Command: []string{"sh", "-c", "sleep 3600"},
                }},
            RestartPolicy: v1.RestartPolicyNever,
        },
    }

    pod := f.PodClient().CreateSync(podDesc)

    return pod
}

var _ = framework.KubeDescribe("Ephemeral Containers [NodeFeature:EphemeralContainers]", func() {
    f := framework.NewDefaultFramework("ephemeral-containers-tests")
    ginkgo.Context("With a pod of running state", func() {
        tempSetCurrentKubeletConfig(f, func(initialConfig *kubeletconfig.KubeletConfiguration) {
            defer withFeatureGate(features.EphemeralContainers, true)()
            if initialConfig.FeatureGates == nil {
                initialConfig.FeatureGates = make(map[string]bool)
            }
            initialConfig.FeatureGates[string(features.EphemeralContainers)] = true
        })

        ginkgo.Context("", func() {
            ginkgo.It("creates a static pod with ephemeral container", func() {
                err := createStaticPodWithEphemeralContainer(f)
                framework.ExpectNoError(err)
                mirrorPodName := "test-static-pod" + "-" + framework.TestContext.NodeName
                gomega.Eventually(func() error {
                    pod, err := f.PodClient().Get(context.TODO(), mirrorPodName, metav1.GetOptions{})
                    if err != nil {
                        return err
                    }
                    if pod.Status.Phase != v1.PodRunning {
                        return fmt.Errorf("expected the mirror pod %q to be running, got %q", "test-static-pod", pod.Status.Phase)
                    }
                    klog.Infoln(pod)
                    return nil
                }, 2*time.Minute, time.Second*4).Should(gomega.BeNil())
            })

        })

        ginkgo.Context("", func() {

            var pod *v1.Pod

            ginkgo.BeforeEach(func() {
                pod = createPodWithoutEphemeralContainer(f)
                err := f.WaitForPodRunning(pod.Name)
                framework.ExpectNoError(err)
            })

            ginkgo.AfterEach(func() {
                deletePodsSync(f, []*v1.Pod{pod})
            })

            ginkgo.It("should add and start ephemeral container to the pod", func() {
                ephemeralContainer := v1.EphemeralContainer{
                    EphemeralContainerCommon: v1.EphemeralContainerCommon{
                        Name:                     "ephemeral-1",
                        Image:                    busyboxImage,
                        Command:                  []string{"sh"},
                        TTY:                      true,
                        Stdin:                    true,
                        ImagePullPolicy:          v1.PullIfNotPresent,                        // field required for ephemeral containers
                        TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError, // field required for ephemeral containers
                    },
                }
                ginkgo.By("add ephemeral container to pod")

                ctx := context.Background()

                klog.Info(f.PodClient().List(ctx, metav1.ListOptions{}))
                ec, err := f.PodClient().GetEphemeralContainers(ctx, pod.Name, metav1.GetOptions{})

                framework.ExpectNoError(err)

                ec.EphemeralContainers = []v1.EphemeralContainer{ephemeralContainer}
                _, err = f.PodClient().UpdateEphemeralContainers(ctx, pod.Name, ec, metav1.UpdateOptions{})

                framework.ExpectNoError(err)

                ginkgo.By("wait for ephemeral container to be ready")
                gomega.Eventually(func() error {
                    pod, err := f.PodClient().Get(ctx, pod.Name, metav1.GetOptions{})
                    if err != nil {
                        return err
                    }
                    statuses := pod.Status.EphemeralContainerStatuses
                    if len(statuses) != 1 {
                        return fmt.Errorf("unexpected multiple ContainerStatus: %v", statuses)
                    }
                    status := statuses[0]
                    if status.State.Running == nil {
                        return fmt.Errorf("ephemeral container not yet running")
                    }
                    return nil
                }, 30*time.Second, framework.Poll).Should(gomega.BeNil())
            })

        })

    })
})
