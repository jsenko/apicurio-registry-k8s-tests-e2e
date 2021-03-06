package streams

import (
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/Apicurio/apicurio-registry-k8s-tests-e2e/testsuite/utils"
	apicurioutils "github.com/Apicurio/apicurio-registry-k8s-tests-e2e/testsuite/utils/apicurio"
	"github.com/Apicurio/apicurio-registry-k8s-tests-e2e/testsuite/utils/kubernetescli"
	"github.com/Apicurio/apicurio-registry-k8s-tests-e2e/testsuite/utils/types"

	apicurio "github.com/Apicurio/apicurio-registry-operator/pkg/apis/apicur/v1alpha1"
)

var log = logf.Log.WithName("streams")

var bundlePath string = utils.StrimziOperatorBundlePath

var registryName string

//DeployStreamsRegistry deploys a kafka cluster using strimzi operator and deploys an ApicurioRegistry CR using the kafka cluster
func DeployStreamsRegistry(suiteCtx *types.SuiteContext, ctx *types.TestContext) {

	kafkaRequest := &CreateKafkaClusterRequest{
		Name:           "registry-kafka",
		Namespace:      ctx.RegistryNamespace,
		ExposeExternal: false,
		Replicas:       3,
		Topics:         []string{"storage-topic", "global-id-topic"},
		Security:       ctx.Security,
	}

	kafkaClusterInfo := DeployKafkaCluster(suiteCtx, kafkaRequest)

	ctx.KafkaClusterInfo = kafkaClusterInfo

	bootstrapServers := kafkaClusterInfo.BootstrapServers

	log.Info("Deploying apicurio registry")

	replicas := 1
	if ctx.Replicas > 0 {
		replicas = ctx.Replicas
	}

	registryName = "apicurio-registry-" + ctx.Storage
	registry := apicurio.ApicurioRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name: registryName,
		},
		Spec: apicurio.ApicurioRegistrySpec{
			Configuration: apicurio.ApicurioRegistrySpecConfiguration{
				LogLevel:    "DEBUG",
				Persistence: utils.StorageStreams,
				Streams: apicurio.ApicurioRegistrySpecConfigurationStreams{
					ApplicationId:    "registry-application-id",
					BootstrapServers: bootstrapServers,
				},
			},
			Deployment: apicurio.ApicurioRegistrySpecDeployment{
				Replicas: int32(replicas),
			},
		},
	}

	if ctx.Security == "tls" {
		truststoreSecret := kafkaRequest.Name + "-cluster-ca-truststore"
		keystoreSecret := kafkaClusterInfo.Username + "-keystore"

		scriptFile := utils.SuiteProjectDir + "/scripts/kafka/create_cert_stores.sh"
		err := utils.ExecuteCmd(true, &utils.Command{
			Env: []string{
				"CLUSTER_CA_CERT_SECRET=" + kafkaRequest.Name + "-cluster-ca-cert",
				"CLIENT_CERT_SECRET=" + kafkaClusterInfo.Username,
				"TRUSTSTORE_SECRET=" + truststoreSecret,
				"KEYSTORE_SECRET=" + keystoreSecret,
				"HOSTNAME=" + kafkaRequest.Name + "-kafka-bootstrap",
				"NAMESPACE=" + ctx.RegistryNamespace,
				"K8S_CMD=" + string(kubernetescli.GetCLIKubernetesClient().Cmd),
			},
			Cmd: []string{scriptFile},
		})
		Expect(err).ToNot(HaveOccurred())
		registry.Spec.Configuration.Streams.Security.Tls.KeystoreSecretName = keystoreSecret
		registry.Spec.Configuration.Streams.Security.Tls.TruststoreSecretName = truststoreSecret

		defer utils.ExecuteCmd(true, &utils.Command{Cmd: []string{utils.SuiteProjectDir + "/scripts/kafka/clean_certs.sh"}})

	} else if ctx.Security == "scram" {
		truststoreSecret := kafkaRequest.Name + "-cluster-ca-truststore"

		scriptFile := utils.SuiteProjectDir + "/scripts/kafka/create_cert_stores.sh"
		err := utils.ExecuteCmd(true, &utils.Command{
			Env: []string{
				"CLUSTER_CA_CERT_SECRET=" + kafkaRequest.Name + "-cluster-ca-cert",
				"TRUSTSTORE_SECRET=" + truststoreSecret,
				"NAMESPACE=" + ctx.RegistryNamespace,
				"K8S_CMD=" + string(kubernetescli.GetCLIKubernetesClient().Cmd),
			},
			Cmd: []string{scriptFile},
		})
		Expect(err).ToNot(HaveOccurred())
		registry.Spec.Configuration.Streams.Security.Scram.TruststoreSecretName = truststoreSecret
		registry.Spec.Configuration.Streams.Security.Scram.PasswordSecretName = kafkaClusterInfo.Username
		registry.Spec.Configuration.Streams.Security.Scram.User = kafkaClusterInfo.Username

		defer utils.ExecuteCmd(true, &utils.Command{Cmd: []string{utils.SuiteProjectDir + "/scripts/kafka/clean_certs.sh"}})

	}

	apicurioutils.CreateRegistryAndWait(suiteCtx, ctx, &registry)

}

//RemoveStreamsRegistry uninstalls registry CR, kafka cluster and strimzi operator
func RemoveStreamsRegistry(suiteCtx *types.SuiteContext, ctx *types.TestContext) {

	defer os.Remove(bundlePath)

	apicurioutils.DeleteRegistryAndWait(suiteCtx, ctx.RegistryNamespace, registryName)

	if ctx.Security == "tls" {
		kubernetescli.Execute("delete", "secret", ctx.KafkaClusterInfo.Name+"-cluster-ca-truststore", "-n", ctx.RegistryNamespace)
		kubernetescli.Execute("delete", "secret", ctx.KafkaClusterInfo.Username+"-keystore", "-n", ctx.RegistryNamespace)
	} else if ctx.Security == "scram" {
		kubernetescli.Execute("delete", "secret", ctx.KafkaClusterInfo.Name+"-cluster-ca-truststore", "-n", ctx.RegistryNamespace)
	}

	RemoveKafkaCluster(suiteCtx.Clientset, ctx.RegistryNamespace, ctx.KafkaClusterInfo)

	RemoveStrimziOperator(suiteCtx.Clientset, ctx.RegistryNamespace)

}

type CreateKafkaClusterRequest struct {
	Namespace      string
	Replicas       int
	ExposeExternal bool
	Name           string
	Topics         []string
	Security       string
}

//DeployKafkaCluster deploys a kafka cluster and some topics, returns a flag to indicate if strimzi operator has been deployed(useful to know if it was already installed)
func DeployKafkaClusterV2(suiteCtx *types.SuiteContext, namespace string, replicas int, exposeExternal bool, name string, topics []string) *types.KafkaClusterInfo {
	return DeployKafkaCluster(suiteCtx,
		&CreateKafkaClusterRequest{
			Name:           name,
			ExposeExternal: exposeExternal,
			Namespace:      namespace,
			Replicas:       replicas,
			Topics:         topics,
		})
}

func DeployKafkaCluster(suiteCtx *types.SuiteContext, req *CreateKafkaClusterRequest) *types.KafkaClusterInfo {

	strimziDeployed := deployStrimziOperator(suiteCtx.Clientset, req.Namespace)

	clusterInfo := &types.KafkaClusterInfo{StrimziDeployed: strimziDeployed}

	clusterInfo.Name = req.Name
	clusterInfo.Topics = req.Topics

	if req.Security == "tls" || req.Security == "scram" {
		authType := "tls"
		if req.Security == "scram" {
			authType = "scram-sha-512"
		}
		clusterInfo.AuthType = authType
	}
	var replicasStr string = strconv.Itoa(req.Replicas)
	minisr := "1"
	if req.Replicas > 1 {
		minisr = "2"
	}
	var kafkaClusterManifest string = ""
	kindBoostrapHost := "bootstrap.127.0.0.1.nip.io"
	if req.ExposeExternal {
		brokerHost := "broker-0.127.0.0.1.nip.io"
		template := "kafka-cluster-external-template.yaml"
		if suiteCtx.IsOpenshift {
			template = "kafka-cluster-external-ocp-template.yaml"
		}

		kafkaClusterManifestFile := utils.Template("kafka-cluster",
			utils.SuiteProjectDir+"/kubefiles/"+template,
			utils.Replacement{Old: "{NAMESPACE}", New: req.Namespace},
			utils.Replacement{Old: "{NAME}", New: req.Name},
			utils.Replacement{Old: "{BOOTSTRAP_HOST}", New: kindBoostrapHost},
			utils.Replacement{Old: "{BROKER_HOST}", New: brokerHost},
		)
		kafkaClusterManifest = kafkaClusterManifestFile.Name()
	} else {

		if req.Security == "" {
			kafkaClusterManifestFile := utils.Template("kafka-cluster",
				utils.SuiteProjectDir+"/kubefiles/kafka-cluster-template.yaml",
				utils.Replacement{Old: "{NAMESPACE}", New: req.Namespace},
				utils.Replacement{Old: "{NAME}", New: req.Name},
				utils.Replacement{Old: "{REPLICAS}", New: replicasStr},
				utils.Replacement{Old: "{MIN_ISR}", New: minisr},
			)
			kafkaClusterManifest = kafkaClusterManifestFile.Name()
		} else if req.Security == "tls" || req.Security == "scram" {
			kafkaClusterManifestFile := utils.Template("kafka-cluster",
				utils.SuiteProjectDir+"/kubefiles/kafka-cluster-secured-template.yaml",
				utils.Replacement{Old: "{NAMESPACE}", New: req.Namespace},
				utils.Replacement{Old: "{NAME}", New: req.Name},
				utils.Replacement{Old: "{REPLICAS}", New: replicasStr},
				utils.Replacement{Old: "{MIN_ISR}", New: minisr},
				utils.Replacement{Old: "{AUTH_TYPE}", New: clusterInfo.AuthType},
			)
			kafkaClusterManifest = kafkaClusterManifestFile.Name()
		} else {
			Expect(errors.NewBadRequest("uknown security method")).NotTo(HaveOccurred())
		}

	}

	log.Info("Deploying kafka cluster " + req.Name)
	kubernetescli.Execute("apply", "-f", kafkaClusterManifest, "-n", req.Namespace)

	for _, topic := range req.Topics {
		kafkaTopicManifestFile := utils.Template("kafka-topic-"+topic,
			utils.SuiteProjectDir+"/kubefiles/kafka-topic-template.yaml",
			utils.Replacement{Old: "{NAMESPACE}", New: req.Namespace},
			utils.Replacement{Old: "{TOPIC_NAME}", New: topic},
			utils.Replacement{Old: "{CLUSTER_NAME}", New: req.Name},
			utils.Replacement{Old: "{REPLICAS}", New: replicasStr},
			utils.Replacement{Old: "{PARTITIONS}", New: replicasStr},
		)
		kafkaTopicManifest := kafkaTopicManifestFile.Name()

		log.Info("Deploying kafka topic " + topic)
		kubernetescli.Execute("apply", "-f", kafkaTopicManifest, "-n", req.Namespace)
	}

	if req.Security == "tls" || req.Security == "scram" {
		log.Info("Creating secured kafka user")
		kafkaUserName := "registry-user-secured"
		clusterInfo.Username = kafkaUserName
		kafkaUserFile := utils.Template("kafka-user",
			utils.SuiteProjectDir+"/kubefiles/kafka-user-secured-template.yaml",
			utils.Replacement{Old: "{USER_NAME}", New: kafkaUserName},
			utils.Replacement{Old: "{NAMESPACE}", New: req.Namespace},
			utils.Replacement{Old: "{CLUSTER_NAME}", New: req.Name},
			utils.Replacement{Old: "{AUTH_TYPE}", New: clusterInfo.AuthType},
		)
		kubernetescli.Execute("apply", "-f", kafkaUserFile.Name(), "-n", req.Namespace)
	}

	//wait for kafka cluster
	//TODO make this timeout configurable
	timeout := 10 * time.Minute
	log.Info("Waiting for kafka cluster to be ready ", "timeout", timeout)
	err := wait.Poll(utils.APIPollInterval, timeout, func() (bool, error) {
		od, err := suiteCtx.Clientset.AppsV1().Deployments(req.Namespace).Get(req.Name+"-entity-operator", metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}
		if od != nil {
			if od.Status.AvailableReplicas > int32(0) {
				return true, nil
			}
		}
		return false, nil
	})
	kubernetescli.GetDeployments(req.Namespace)
	kubernetescli.GetPods(req.Namespace)
	kubernetescli.GetVolumes(req.Namespace)
	Expect(err).ToNot(HaveOccurred())

	if req.Security == "tls" || req.Security == "scram" {
		//wait for required cluster ca secret
		timeout := 1 * time.Minute
		log.Info("Waiting for kafka cluster CA secret to be created ", "timeout", timeout)
		err := wait.Poll(utils.APIPollInterval, timeout, func() (bool, error) {
			_, err := suiteCtx.Clientset.CoreV1().Secrets(req.Namespace).Get(req.Name+"-cluster-ca-cert", metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		})
		kubernetescli.Execute("get", "secret", "-n", req.Namespace)
		Expect(err).ToNot(HaveOccurred())
	}

	if req.ExposeExternal {
		clusterInfo.ExternalBootstrapServers = kindBoostrapHost + ":443"
		if suiteCtx.IsOpenshift {
			route, err := suiteCtx.OcpRouteClient.Routes(req.Namespace).Get(req.Name+"-kafka-bootstrap", metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(route.Status.Ingress)).ToNot(BeIdenticalTo(0))
			host := route.Status.Ingress[0].Host
			clusterInfo.ExternalBootstrapServers = host + ":443"
		}
	}

	// svc, err := suiteCtx.Clientset.CoreV1().Services(req.Namespace).Get(req.Name+"-kafka-bootstrap", metav1.GetOptions{})
	// Expect(err).ToNot(HaveOccurred())
	bootstrapServers := req.Name + "-kafka-bootstrap." + req.Namespace + ":9092"
	if req.Security != "" {
		bootstrapServers = req.Name + "-kafka-bootstrap." + req.Namespace + ":9093"
	}
	clusterInfo.BootstrapServers = bootstrapServers
	return clusterInfo
}

func deployStrimziOperator(clientset *kubernetes.Clientset, namespace string) bool {

	_, err := clientset.AppsV1().Deployments(namespace).Get("strimzi-cluster-operator", metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		Expect(err).ToNot(HaveOccurred())
	} else if err == nil {
		log.Info("Strimzi operator is already deployed")
		return false
	}

	log.Info("Deploying strimzi operator")

	if strings.HasPrefix(utils.StrimziOperatorBundlePath, "https://") {
		bundlePath = "/tmp/strimzi-operator-bundle-" + strconv.Itoa(rand.Intn(1000)) + ".yaml"
		utils.DownloadFile(bundlePath, utils.StrimziOperatorBundlePath)
		utils.ExecuteCmdOrDie(false, "sed", "-i", "s/namespace: .*/namespace: "+namespace+"/", bundlePath)
	} else {
		filepath.Walk(utils.StrimziOperatorBundlePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				Expect(err).ToNot(HaveOccurred())
			}
			if strings.Contains(info.Name(), "RoleBinding") {
				utils.ExecuteCmdOrDie(true, "sed", "-i", "s/namespace: .*/namespace: "+namespace+"/", path)
			}
			return nil
		})
		bundlePath = utils.StrimziOperatorBundlePath
	}

	kubernetescli.Execute("apply", "-f", bundlePath, "-n", namespace)

	// sh("oc wait deployment/strimzi-cluster-operator --for condition=available --timeout=180s")
	timeout := 120 * time.Second
	log.Info("Waiting for strimzi operator to be ready ", "timeout", timeout)
	err = wait.Poll(utils.APIPollInterval, timeout, func() (bool, error) {
		od, err := clientset.AppsV1().Deployments(namespace).Get("strimzi-cluster-operator", metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}
		if od != nil {
			if od.Status.AvailableReplicas > int32(0) {
				return true, nil
			}
		}
		return false, nil
	})
	kubernetescli.GetPods(namespace)
	Expect(err).ToNot(HaveOccurred())
	return true
}

//RemoveKafkaCluster removes a kafka cluster
func RemoveKafkaCluster(clientset *kubernetes.Clientset, namespace string, kafkaClusterInfo *types.KafkaClusterInfo) {

	log.Info("Removing kafka cluster")

	kubernetescli.Execute("delete", "kafka", kafkaClusterInfo.Name, "-n", namespace)
	for _, topic := range kafkaClusterInfo.Topics {
		kubernetescli.Execute("delete", "kafkatopic", topic, "-n", namespace)
	}

	if kafkaClusterInfo.Username != "" {
		kubernetescli.Execute("delete", "kafkauser", kafkaClusterInfo.Username, "-n", namespace)
	}

	timeout := 120 * time.Second
	log.Info("Waiting for kafka cluster to be removed ", "timeout", timeout)
	err := wait.Poll(utils.APIPollInterval, timeout, func() (bool, error) {
		labelsSet := labels.Set(map[string]string{"strimzi.io/cluster": kafkaClusterInfo.Name})
		l, err := clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: labelsSet.AsSelector().String()})
		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return len(l.Items) == 0, nil
	})
	kubernetescli.GetDeployments(namespace)
	kubernetescli.GetStatefulSets(namespace)
	kubernetescli.GetPods(namespace)
	kubernetescli.GetVolumes(namespace)
	Expect(err).ToNot(HaveOccurred())

}

//RemoveStrimziOperator uninstalls strimzi operator
func RemoveStrimziOperator(clientset *kubernetes.Clientset, namespace string) {
	log.Info("Removing strimzi operator")
	kubernetescli.Execute("delete", "-f", bundlePath, "-n", namespace)

	timeout := 120 * time.Second
	log.Info("Waiting for strimzi cluster operator to be removed ", "timeout", timeout)
	err := wait.Poll(utils.APIPollInterval, timeout, func() (bool, error) {
		_, err := clientset.AppsV1().Deployments(namespace).Get("strimzi-cluster-operator", metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
	kubernetescli.GetDeployments(namespace)
	kubernetescli.GetPods(namespace)
	Expect(err).ToNot(HaveOccurred())
}
