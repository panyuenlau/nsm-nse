package common

import (
	"bufio"
	"bytes"
	"io"
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"path/filepath"
	"strings"
	"os/exec"
	"testing"
	"time"
)

func ExecKindCluster(action, clusterName string) error {
	nameFlag := "--name=" + clusterName
	clusterCmd := exec.Command("kind", action, "cluster", nameFlag)
	err := clusterCmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func CreateCluster(clusterName *string) error {
	// Remove existing cluster with the same name
	if err := RemoveExistingCluster(clusterName); err != nil {
		return err
	}

	// Create a kind cluster for testing
	err := ExecKindCluster("create", *clusterName)
	if err != nil {
		return err
	}
	return nil
}

func RemoveExistingCluster(clusterName *string) error {
	out, err := exec.Command("kind", "get", "clusters").Output()
	if err != nil {
		return err
	}

	//Check if input cluster already exists
	//If found, remove it
	s := strings.Fields(string(out))
	for _, name := range s {
		if name == *clusterName {
			if err = ExecKindCluster("delete", *clusterName); err != nil {
				return err
			}
		}
	}
	return nil
}

func GetClientSet(kubeconfig string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

// Get the cluster's kubeconfig: kind get kubeconfig --name=${kind-kind-1} > ${localCluster}
func GetKubeconfig(clusterName string) (string, error) {
	cmd := "kind get kubeconfig --name=${kind-" + clusterName + "}"

	bashCmd, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return "", err
	}
	curDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	kubeconfig := filepath.Join(curDir, filepath.Base(clusterName+".kubeconfig"))

	outfile, err := os.Create(kubeconfig)

	if err != nil {
		panic(err)
		return "", err
	}
	defer outfile.Close()

	writer := bufio.NewWriter(outfile)
	defer writer.Flush()

	_, err = io.Copy(writer, bytes.NewReader(bashCmd))
	if err != nil {
		return "", err
	}
	return kubeconfig, nil
}

func GetPods(clientset *kubernetes.Clientset, namespace string, selectors ...string) ([]corev1.Pod, error) {
	s := strings.Join(selectors, ",")
	list, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: s})
	if err != nil {
		return []corev1.Pod{}, err
	}
	return list.Items, nil
}

// RetryExecution executes a given function until it succeeds or the max number of tries is reached
func RetryExecution(t *testing.T, maxTries int, executable func() error) (err error) {
	for i := 0; i < maxTries; i++ {
		err = executable()
		if err == nil {
			break
		}
		retryTime := 1 + i*i
		t.Logf("Operation failed: retrying after %d seconds", retryTime)
		time.Sleep(time.Duration(retryTime) * time.Second)
	}
	return err
}

func ExecScript(script string) error {
	cmd := exec.Command("bash", "-c", script)
	err := cmd.Run()
	return err
}