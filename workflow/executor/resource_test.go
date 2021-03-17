package executor

import (
	"context"
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"

	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/workflow/executor/mocks"
)

// TestResourceFlags tests whether Resource Flags
// are properly passed to `kubectl` command
func TestResourceFlags(t *testing.T) {
	manifestPath := "../../examples/hello-world.yaml"
	fakeClientset := fake.NewSimpleClientset()
	fakeFlags := []string{"--fake=true"}

	mockRuntimeExecutor := mocks.ContainerRuntimeExecutor{}

	template := wfv1.Template{
		Resource: &wfv1.ResourceTemplate{
			Action: "fake",
			Flags:  fakeFlags,
		},
	}

	we := WorkflowExecutor{
		PodName:            fakePodName,
		Template:           template,
		ClientSet:          fakeClientset,
		Namespace:          fakeNamespace,
		PodAnnotationsPath: fakeAnnotations,
		ExecutionControl:   nil,
		RuntimeExecutor:    &mockRuntimeExecutor,
	}
	args, err := we.getKubectlArguments("fake", manifestPath, fakeFlags)
	assert.NoError(t, err)
	assert.Contains(t, args, fakeFlags[0])

	_, err = we.getKubectlArguments("fake", manifestPath, nil)
	assert.NoError(t, err)
	_, err = we.getKubectlArguments("fake", "unknown-location", fakeFlags)
	assert.EqualError(t, err, "open unknown-location: no such file or directory")

	emptyFile, err := ioutil.TempFile("/tmp", "empty-manifest")
	assert.NoError(t, err)
	defer func() { _ = os.Remove(emptyFile.Name()) }()
	_, err = we.getKubectlArguments("fake", emptyFile.Name(), nil)
	assert.EqualError(t, err, "Must provide at least one of flags or manifest.")
}

// TestResourcePatchFlags tests whether Resource Flags
// are properly passed to `kubectl patch` command
func TestResourcePatchFlags(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	manifestPath := "../../examples/hello-world.yaml"
	buff, err := ioutil.ReadFile(manifestPath)
	assert.NoError(t, err)
	fakeFlags := []string{"patch", "--type", "strategic", "-p", string(buff), "-o", "json"}

	mockRuntimeExecutor := mocks.ContainerRuntimeExecutor{}

	template := wfv1.Template{
		Resource: &wfv1.ResourceTemplate{
			Action: "patch",
			Flags:  fakeFlags,
		},
	}

	we := WorkflowExecutor{
		PodName:            fakePodName,
		Template:           template,
		ClientSet:          fakeClientset,
		Namespace:          fakeNamespace,
		PodAnnotationsPath: fakeAnnotations,
		ExecutionControl:   nil,
		RuntimeExecutor:    &mockRuntimeExecutor,
	}
	args, err := we.getKubectlArguments("patch", manifestPath, nil)

	assert.NoError(t, err)
	assert.Equal(t, args, fakeFlags)
}

// TestResourceConditionsMatching tests whether the JSON response match
// with either success or failure conditions.
func TestResourceConditionsMatching(t *testing.T) {
	var successReqs labels.Requirements
	successSelector, err := labels.Parse("status.phase == Succeeded")
	assert.NoError(t, err)
	successReqs, _ = successSelector.Requirements()
	assert.NoError(t, err)
	var failReqs labels.Requirements
	failSelector, err := labels.Parse("status.phase == Error")
	assert.NoError(t, err)
	failReqs, _ = failSelector.Requirements()
	assert.NoError(t, err)

	jsonBytes := []byte(`{"name": "test","status":{"phase":"Error"}`)
	finished, err := matchConditions(jsonBytes, successReqs, failReqs)
	assert.Error(t, err, `failure condition '{status.phase == [Error]}' evaluated true`)
	assert.False(t, finished)

	jsonBytes = []byte(`{"name": "test","status":{"phase":"Succeeded"}`)
	finished, err = matchConditions(jsonBytes, successReqs, failReqs)
	assert.NoError(t, err)
	assert.False(t, finished)

	jsonBytes = []byte(`{"name": "test","status":{"phase":"Pending"}`)
	finished, err = matchConditions(jsonBytes, successReqs, failReqs)
	assert.Error(t, err, "Neither success condition nor the failure condition has been matched. Retrying...")
	assert.True(t, finished)
}

func TestWorks(t *testing.T) {
	//selfLink := "/apis/argoproj.io/v1alpha1/namespaces/kubemaker/workflows/dag-diamond-coinflip-77q9b"
	config, err := clientcmd.BuildConfigFromFlags("", "/Users/yuan.tang/.kube/config")
	client, err := kubernetes.NewForConfig(config)
	assert.NoError(t, err)
	restClient := client.RESTClient()

	//var successReqs labels.Requirements
	//successSelector, err := labels.Parse("status.phase == Succeeded")
	//assert.NoError(t, err)
	//successReqs, _ = successSelector.Requirements()
	//assert.NoError(t, err)
	//var failReqs labels.Requirements
	//failSelector, err := labels.Parse("status.phase == Error")
	//assert.NoError(t, err)
	//failReqs, _ = failSelector.Requirements()
	//assert.NoError(t, err)
	//
	//link := strings.Split(selfLink, "/")
	//plural := link[len(link)-2]
	//gvr := schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: plural}
	// TODO: jsonString is null
	//var data io.ReadCloser
	//data, err = os.Open("../../examples/k8s-owner-reference.yaml")
	//assert.NoError(t, err)
	dataBytes := []byte(`apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: owned-eg-
spec:
  entrypoint: main
  templates:
    - name: main
      container:
        image: argoproj/argosay:v2`)
	request := restClient.Post().RequestURI("/apis/argoproj.io/v1alpha1/namespaces/argo/workflows").SetHeader("Content-Type", "application/yaml").Body(dataBytes)
	stream, err := request.Stream(context.TODO())
	assert.NoError(t, err)
	defer stream.Close()
	jsonBytes, err := ioutil.ReadAll(stream)
	assert.NoError(t, err)
	log.Info(string(jsonBytes))

	obj := unstructured.Unstructured{}
	err = json.Unmarshal(jsonBytes, &obj)
	assert.NoError(t, err)
	resourceName := fmt.Sprintf("%s.%s/%s", obj.GroupVersionKind().Kind, obj.GroupVersionKind().Group, obj.GetName())
	selfLink := obj.GetSelfLink()
	log.Infof("Resource: %s/%s. SelfLink: %s", obj.GetNamespace(), resourceName, selfLink)

	dataBytes = []byte("")
	request = restClient.Delete().RequestURI(selfLink).SetHeader("Content-Type", "application/yaml").Body(dataBytes)
	stream, err = request.Stream(context.TODO())
	assert.NoError(t, err)
	defer stream.Close()
	jsonBytes, err = ioutil.ReadAll(stream)
	assert.NoError(t, err)
	log.Info(string(jsonBytes))
	//_, err = checkResourceState(dynamicClient, context.TODO(), "kubemaker", "dag-diamond-coinflip-77q9b", gvr, successReqs, failReqs)
}
