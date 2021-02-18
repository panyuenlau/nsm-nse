package chained_ns_test

import (
	"flag"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	//if home := homedir.HomeDir(); home != "" {
	//	kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	//} else {
	//	kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	//}
	flag.Parse()

	os.Exit(m.Run())
}
