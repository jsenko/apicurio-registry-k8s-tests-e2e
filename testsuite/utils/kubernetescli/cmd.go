package kubernetescli

import (
	"os"
	"sync"

	. "github.com/onsi/gomega"

	utils "github.com/Apicurio/apicurio-registry-k8s-tests-e2e/testsuite/utils"
)

type CLIKubernetesClient string

var (
	Kubectl CLIKubernetesClient = "kubectl"
	Oc      CLIKubernetesClient = "oc"
)

type KubernetesClient struct {
	Cmd CLIKubernetesClient
}

var lock = &sync.Mutex{}

var instance *KubernetesClient

func NewCLIKubernetesClient(cmd CLIKubernetesClient) *KubernetesClient {

	lock.Lock()
	defer lock.Unlock()

	if instance == nil {
		instance = &KubernetesClient{
			Cmd: cmd,
		}
	}

	return instance
}

func GetCLIKubernetesClient() *KubernetesClient {
	Expect(instance).ToNot(BeNil())
	return instance
}

func GetDeployments(namespace string) {
	Execute("get", "deployment", "-n", namespace)
}

func GetStatefulSets(namespace string) {
	Execute("get", "statefulset", "-n", namespace)
}

func GetPods(namespace string) {
	Execute("get", "pod", "-n", namespace)
}

func GetVolumes(namespace string) {
	Execute("get", "pvc", "-n", namespace)
	Execute("get", "pv")
}

func Execute(args ...string) {
	ExecuteCmd(true, args...)
}

func ExecuteCmd(logOutput bool, args ...string) {
	utils.ExecuteCmdOrDieCore(logOutput, logOutput, string(GetCLIKubernetesClient().Cmd), args...)
}

func RedirectOutput(stdOutFile *os.File, stdErrFile *os.File, args ...string) {
	err := utils.Execute(&utils.Command{Cmd: append([]string{string(GetCLIKubernetesClient().Cmd)}, args...)}, stdOutFile, stdErrFile, true)
	Expect(err).ToNot(HaveOccurred())
}
