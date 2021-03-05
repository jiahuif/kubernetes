package leadermigration

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	v1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/watch"
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	clientset "k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	ccmtesting "k8s.io/cloud-provider/app/testing"
	"k8s.io/cloud-provider/fake"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/controller-manager/pkg/features"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	kcmtesting "k8s.io/kubernetes/cmd/kube-controller-manager/app/testing"
	"k8s.io/kubernetes/test/integration/framework"
)

var cloudInitChan chan struct{}

func init() {
	cloudInitChan = make(chan struct{})
}
func TestMain(m *testing.M) {
	framework.EtcdMain(m.Run)
}

func TestLeaderMigration(t *testing.T) {
	cloudprovider.RegisterCloudProvider("fake", fakeCloudProviderFactory)

	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.ControllerManagerLeaderMigration, true)()

	// Insulate this test from picking up in-cluster config when run inside a pod
	// We can't assume we have permissions to write to /var/run/secrets/... from a unit test to mock in-cluster config for testing
	originalHost := os.Getenv("KUBERNETES_SERVICE_HOST")
	if len(originalHost) > 0 {
		os.Setenv("KUBERNETES_SERVICE_HOST", "")
		defer os.Setenv("KUBERNETES_SERVICE_HOST", originalHost)
	}

	// authenticate to apiserver via bearer token
	token := "flwqkenfjasasdfmwerasd" // Fake token for testing.
	tokenFile, err := ioutil.TempFile("", "kubeconfig")
	if err != nil {
		t.Fatal(err)
	}
	tokenFile.WriteString(fmt.Sprintf(`
%s,system:kube-controller-manager,system:kube-controller-manager,""
`, token))
	tokenFile.Close()

	// start apiserver
	server := kubeapiservertesting.StartTestServerOrDie(t, nil, []string{
		"--token-auth-file", tokenFile.Name(),
		"--authorization-mode", "RBAC",
	}, framework.SharedEtcd())
	defer server.TearDownFn()

	// create kubeconfig for the apiserver
	apiserverConfig, err := ioutil.TempFile("", "kubeconfig")
	if err != nil {
		t.Fatal(err)
	}
	apiserverConfig.WriteString(fmt.Sprintf(`
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
    certificate-authority: %s
  name: integration
contexts:
- context:
    cluster: integration
    user: controller-manager
  name: default-context
current-context: default-context
users:
- name: controller-manager
  user:
    token: %s
`, server.ClientConfig.Host, server.ServerOpts.SecureServing.ServerCert.CertKey.CertFile, token))
	apiserverConfig.Close()
	client, err := clientset.NewForConfig(server.ClientConfig)
	if err != nil {
		t.Fatal(err)
	}

	if err = grantAccessToLeases(client); err != nil {
		t.Fatal(err)
	}

	migrationFromConfigFile, err := ioutil.TempFile("", "migration-from-config")
	if err != nil {
		t.Fatal(err)
	}
	migrationFromConfigFile.WriteString(`
kind: LeaderMigrationConfiguration
apiVersion: controllermanager.config.k8s.io/v1alpha1
leaderName: cloud-provider-extraction-migration
resourceLock: leases
controllerLeaders:
  - name: route
    component: kube-controller-manager
  - name: service
    component: kube-controller-manager
  - name: cloud-node-lifecycle
    component: kube-controller-manager
`)
	defer os.Remove(migrationFromConfigFile.Name())

	migrationToConfigFile, err := ioutil.TempFile("", "migration-to-config")
	if err != nil {
		t.Fatal(err)
	}
	migrationToConfigFile.WriteString(`
kind: LeaderMigrationConfiguration
apiVersion: controllermanager.config.k8s.io/v1alpha1
leaderName: cloud-provider-extraction-migration
resourceLock: leases
controllerLeaders:
  - name: route
    component: cloud-controller-manager
  - name: service
    component: cloud-controller-manager
  - name: cloud-node-lifecycle
    component: cloud-controller-manager
`)
	defer os.Remove(migrationToConfigFile.Name())

	leaseWatch, err := client.CoordinationV1().Leases("kube-system").Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer leaseWatch.Stop()
	leaseHolderChan := make(chan string)
	go func() {
		var lastHolder string
		for {
			event, ok := <-leaseWatch.ResultChan()
			if !ok {
				return
			}
			if event.Type == watch.Added || event.Type == watch.Modified {
				lease := event.Object.(*v1.Lease)
				if lease.Name == "cloud-provider-extraction-migration" {
					holder := lease.Spec.HolderIdentity
					if holder != nil && lastHolder != *holder {
						leaseHolderChan <- *holder
						lastHolder = *holder
					}
				}
			}
		}
	}()
	// Start a KCM with internal cloud provider
	oldKCM, err := kcmtesting.StartTestServer(t, []string{
		"--kubeconfig=" + apiserverConfig.Name(),
		"--secure-port=0",
		"--port=10253",
		"--leader-elect=true",
		"--enable-leader-migration",
		"--cloud-provider=fake",
		"--leader-migration-config=" + migrationFromConfigFile.Name(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Start a CCM
	oldCCM, err := ccmtesting.StartTestServer(t, []string{
		"--kubeconfig=" + apiserverConfig.Name(),
		"--secure-port=0",
		"--port=10254",
		"--leader-elect=true",
		"--enable-leader-migration",
		"--cloud-provider=fake",
		"--leader-migration-config=" + migrationFromConfigFile.Name(),
	})
	if err != nil {
		t.Fatal(err)
	}

	<-leaseHolderChan

	// Start a new KCM with external cloud provider
	newKCM, err := kcmtesting.StartTestServer(t, []string{
		"--kubeconfig=" + apiserverConfig.Name(),
		"--secure-port=0",
		"--port=10253",
		"--leader-elect=true",
		"--enable-leader-migration",
		"--cloud-provider=external",
		"--leader-migration-config=" + migrationToConfigFile.Name(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer newKCM.TearDownFn()

	// Start a new CCM
	newCCM, err := ccmtesting.StartTestServer(t, []string{
		"--kubeconfig=" + apiserverConfig.Name(),
		"--secure-port=0",
		"--port=10254",
		"--leader-elect=true",
		"--enable-leader-migration",
		"--cloud-provider=fake",
		"--leader-migration-config=" + migrationToConfigFile.Name(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer newCCM.TearDownFn()

	// Bring down old KCM and CCM
	oldKCM.TearDownFn()
	oldCCM.TearDownFn()

	<-leaseHolderChan

}

func fakeCloudProviderFactory(io.Reader) (cloudprovider.Interface, error) {
	return &fake.Cloud{
		DisableRoutes: true, // disable routes for server tests, otherwise --cluster-cidr is required
	}, nil
}

func grantAccessToLeases(client *clientset.Clientset) error {
	_, err := client.RbacV1().Roles("kube-system").Patch(context.Background(),
		"system::leader-locking-kube-controller-manager", types.MergePatchType,
		[]byte(`{"rules": [ {"apiGroups":[ "coordination.k8s.io"], "resources": ["leases"], "verbs": ["create", "list", "get", "update"] } ]}`),
		metav1.PatchOptions{})
	return err
}
