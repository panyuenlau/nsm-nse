package chained_ns_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"github.com/cisco-app-networking/nsm-nse/test/utils/common"
	. "github.com/onsi/gomega"
	_ "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var (
	replicas    = int32(2)
	serviceName = "vl3-service"

	clientDep = &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "helloworld-" + serviceName,
			Namespace: "default",
			Labels: map[string]string{
				"version": "v1",
			},
			Annotations: map[string]string{
				"ns.networkservicemesh.io": serviceName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":     "helloworld-" + serviceName,
					"version": "v1",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     "helloworld-" + serviceName,
						"version": "v1",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "helloworld",
							Image:           "docker.io/istio/examples-helloworld-v1",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Resources: corev1.ResourceRequirements{
								Limits: nil,
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 5000,
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
		},
	}

	serviceConf = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "helloworld-" + serviceName,
			Labels: map[string]string{
				"app":      "helloworld-" + serviceName,
				"nsm/role": "client",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: 5000,
				},
			},
			Selector: map[string]string{
				"app": "helloworl-" + serviceName,
			},
		},
	}
)

var (
	GOPATH = os.Getenv("GOPATH")
	VL3DIR = GOPATH + "/src/github.com/cisco-app-networking/nsm-nse"

	clusterName = flag.String("clusterName", "test-1", "Name of kind cluster for this test")
	ipAddr      = flag.String("remoteIp", "127.0.0.1", "Set ENV to REMOTE_IP")
	nsmPath     = flag.String("nsmPath", filepath.Join(VL3DIR, "scripts/vl3/nsm_install_interdomain.sh"), "Path of script to install NSM")

	clientset *kubernetes.Clientset

	nsePath = flag.String("vl3-nse Path", filepath.Join(VL3DIR, "scripts/vl3/vl3_interdomain.sh --pass-through"), "Path of script to install NSE")
)

var maxTries = 10

type pod struct {
	name string
	ip   string
}

func TestMultiNscSingleCluster(t *testing.T) {
	g := NewWithT(t)

	t.Log("Running test case: Single Cluster chained NS test")

	// First create the cluster
	t.Logf("Creating cluster `%s`...", *clusterName)
	err := common.CreateCluster(clusterName)
	g.Expect(err).Should(BeNil())

	// Get the cluster's kubeconfig
	t.Logf("Getting kubeconfig for cluster `%s`", *clusterName)
	kubeconfig, err := common.GetKubeconfig(*clusterName)
	g.Expect(err).Should(BeNil())

	// Prepare clientset for K8s API
	clientset, err = common.GetClientSet(kubeconfig)
	g.Expect(err).Should(BeNil())

	// Install NSM
	t.Log("Installing NSM...")
	os.Setenv("KCONF", kubeconfig)

	err = common.ExecScript(*nsmPath)
	g.Expect(err).Should(BeNil())

	// Install NSEs
	os.Setenv("REMOTE_IP", *ipAddr)
	t.Log("Installing NSEs...")
	err = common.ExecScript(*nsePath)
	g.Expect(err).Should(BeNil())

	// Install NSCs
	t.Log("Installing NSCs...")
	clientDeployment := createClientDeployment()
	g.Expect(clientDeployment).ShouldNot(BeNil())

	t.Log("Checking if all client pods are available...")

	err = common.RetryExecution(t, maxTries, checkAvailability(clientDeployment, clientset))
	g.Expect(err).Should(BeNil())

	t.Log(("Client pods are now running! Now ready to check connectivity"))

	// First get the client pods
	clientPods, err := common.GetPods(clientset, clientDeployment.Namespace, "")
	g.Expect(err).Should(BeNil())

	// Then exec into the client pods to get their nsm0 IP address
	pods, err := getPodInfo(clientPods, *clientDeployment, kubeconfig)
	g.Expect(err).Should(BeNil())
	g.Expect(pods).ShouldNot(BeNil())

	// Finally perform connectivity test from one NSC to another one
	err = connectivityTest(t, pods, kubeconfig)
	g.Expect(err).Should(BeNil())
}

func connectivityTest(t *testing.T, pods []pod, kubeconfig string) error {
	var err error
	for i1 := 0; i1 < len(pods) - 1; i1++ {
		p1 := pods[0]
		for i2 := i1 + 1; i2 < len(pods); i2++ {
			p2 := pods[i2]
			err = common.RetryExecution(t, maxTries, curlTo(t, p1, p2, "useIP", kubeconfig))
		}
	}
	return err
}

func getPodInfo(clientPods []corev1.Pod, clientDeployment appsv1.Deployment, kubeconfig string) ([]pod, error) {
	var pods []pod

	m := regexp.MustCompile(`\b(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`)

	for _, p := range clientPods {
		podName := p.Name
		executor, _, err := execPod(clientDeployment.Namespace, podName, "helloworld", kubeconfig, []string{"ip", "a", "show", "dev", "nsm0"}, clientset)
		if err != nil {
			return nil, err
		}

		nsmIP := m.FindString(executor)
		pods = append(pods, pod{
			name: podName,
			ip:   nsmIP,
		})
	}

	return pods, nil
}

func curlTo(t *testing.T, p1, p2 pod, clusterName, kubeconfig string) func() error {
	var err error
	var res string
	return func() error {
		if clusterName == "useIP" {
			res, _, err = execPod("default", p1.name, "helloworld", kubeconfig, []string{"curl", "-v", fmt.Sprintf("http://%s:5000/hello", p2.ip)}, clientset)
			t.Logf("Curl from %s to %s result: %s", p1.name, p2.name, res)
		} else {
			res, _, err = execPod("default", p1.name, "helloworld", kubeconfig, []string{"curl", "-v", fmt.Sprintf("http://helloworld.%s.wcm-cisco.com:5000/hello", clusterName)}, clientset)
			t.Logf("Curl from %s to helloworld.%s.wcm-cisco.com:5000/hello result: %s", p1.name, clusterName, res)
		}
		if err != nil {
			return err
		}
		return nil
	}
}

// Exec executes the provided command on the specified pod/container.
func execPod(namespace, pod, container, kubeconfig string, command []string, clientset *kubernetes.Clientset) (string, string, error) {
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil || info.Size() == 0 {
			fmt.Println("Passed kubeconfig not valid falling back to loading rules")
			kubeconfig = ""
		}
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	loadingRules.ExplicitPath = kubeconfig
	configOverrides := &clientcmd.ConfigOverrides{
		ClusterDefaults: clientcmd.ClusterDefaults,
		CurrentContext:  "",
	}

	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
	if err != nil {
		return "", "", fmt.Errorf("failed to initialize rest config. %w", err)
	}

	restConfig.APIPath = ".api"
	restConfig.GroupVersion = &corev1.SchemeGroupVersion
	restConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec")
	req.VersionedParams(
		&corev1.PodExecOptions{Stdout: true, Stderr: true, Container: container, Command: command},
		scheme.ParameterCodec,
	)
	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return "", "", err
	}
	stdOut := &bytes.Buffer{}
	stdErr := &bytes.Buffer{}
	if err = executor.Stream(remotecommand.StreamOptions{Stdout: stdOut, Stderr: stdErr}); err != nil {
		return "", "", fmt.Errorf("exec stream error: %w", err)
	}

	return stdOut.String(), stdErr.String(), nil
}

func checkAvailability(deployment *appsv1.Deployment, clientset *kubernetes.Clientset) func() error {
	return func() error {
		pods, err := clientset.CoreV1().Pods(deployment.ObjectMeta.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}

		for _, p := range pods.Items {
			if p.Status.Phase != corev1.PodRunning {
				return fmt.Errorf("Pods are not ready yet")
			}
		}
		return nil
	}
}

func createClientDeployment() *appsv1.Deployment {
	deploymentService := clientset.CoreV1().Services("default")
	deploymentsClient := clientset.AppsV1().Deployments(clientDep.GetObjectMeta().GetNamespace())
	clientDeployment, err := deploymentsClient.Create(context.TODO(), clientDep, metav1.CreateOptions{})

	if err != nil {
		return nil
	}

	_, err = deploymentService.Create(context.TODO(), serviceConf, metav1.CreateOptions{})
	if err != nil {
		return nil
	}

	return clientDeployment
}
