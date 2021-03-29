package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"io/ioutil"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var kubeconfig *string

func removeFile(folder, fileName string) bool {
	completeFile := filepath.Join(folder, fileName)
	if _, err := os.Stat(completeFile); !os.IsNotExist(err) {
		os.Remove(completeFile)
		return true
	} else {
		log.Infof("Error: %s file not found", completeFile)
		return false
	}

}

func processConfigMap(item corev1.ConfigMap, destFolder, namespace string, uniqueFilenames, deleted bool) bool {
	var filesChanged bool
	resource := "configmap"

	if item.Data != nil {
		for key, val := range item.Data {
			fileName, fileData := _get_file_data_and_name(key, val, resource)
			if uniqueFilenames {
				fileName = uniqueFilename(fileName, namespace, resource, item.ObjectMeta.Name)
			}
			if !deleted {
				filesChanged = writeTextToFile(destFolder, fileName, fileData)
			} else {
				filesChanged = removeFile(destFolder, fileName)
			}
		}
	}

	if item.BinaryData != nil {
		for key, val := range item.BinaryData {
			fileName, fileData := _get_file_data_and_name(key, string(val), resource)
			if uniqueFilenames {
				fileName = uniqueFilename(fileName, namespace, resource, item.ObjectMeta.Name)
			}
			if !deleted {
				filesChanged = writeTextToFile(destFolder, fileName, fileData)
			} else {
				filesChanged = removeFile(destFolder, fileName)
			}
		}
	}

	return filesChanged
}

func processSecret(item corev1.Secret, destFolder, namespace string, uniqueFilenames, deleted bool) bool {
	var filesChanged bool
	resource := "secret"

	if item.Data != nil {
		for key, val := range item.Data {
			fileName, fileData := _get_file_data_and_name(key, string(val), resource)
			if uniqueFilenames {
				fileName = uniqueFilename(fileName, namespace, resource, item.ObjectMeta.Name)
			}
			if !deleted {
				filesChanged = writeTextToFile(destFolder, fileName, fileData)
			} else {
				filesChanged = removeFile(destFolder, fileName)
			}
		}
	}
	return filesChanged
}

func writeTextToFile(folder, fileName, data string) bool {
	if _, err := os.Stat(folder); os.IsNotExist(err) {
		err := os.MkdirAll(folder, os.ModePerm)
		if err != nil {
			log.Errorf("Error: insufficient privileges to create %s. Skipping %s", folder, fileName)
			return false
		}
	}
	absolutePath := filepath.Join(folder, fileName)
	_, err := os.Stat(absolutePath)
	if os.IsExist(err) {

		data, err := ioutil.ReadFile(absolutePath)
		if err != nil {
			log.Errorf("Error: Failed to read file %s", absolutePath)
		}
		fmt.Print(string(data))
	}

	err = ioutil.WriteFile(absolutePath, []byte(data), 0644)
	if err != nil {
		log.Errorf("Failed to write")
	}

	if val, ok := os.LookupEnv("DEFAULT_FILE_MODE"); ok {

		if err != nil {
			log.Fatal(err)
		}

		mode, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.Chmod(absolutePath, os.FileMode(mode)); err != nil {
			log.Fatal(err)
		}
	}
	return true
}

func request(url, method, payload string) string {
	var retryTotal, retryConnect, retryRead, timeout int
	var retryBackoffFactor float64
	var err error

	val, ok := os.LookupEnv("REQ_RETRY_TOTAL")
	if !ok {
		retryTotal = 5
	} else {
		retryTotal, err = strconv.Atoi(val)
		if err != nil {
			panic(err)
		} else {
			log.Infof("Set retryTotal: %s", retryTotal)
		}
	}

	val, ok = os.LookupEnv("REQ_RETRY_CONNECT")
	if !ok {
		retryConnect = 5
	} else {
		retryConnect, err = strconv.Atoi(val)
		if err != nil {
			panic(err)
		} else {
			log.Infof("Set retryConnect: %s", retryConnect)
		}
	}

	val, ok = os.LookupEnv("REQ_RETRY_READ")
	if !ok {
		retryRead = 5
	} else {
		retryRead, err = strconv.Atoi(val)
		if err != nil {
			panic(err)
		} else {
			log.Infof("Set retryRead: %s", retryRead)
		}
	}

	val, ok = os.LookupEnv("REQ_RETRY_BACKOFF_FACTOR")
	if !ok {
		retryBackoffFactor = 0.2
	} else {
		retryBackoffFactor, err = strconv.ParseFloat(val, 64)
		if err != nil {
			panic(err)
		} else {
			log.Infof("Set retryBackoffFactor: %s", retryBackoffFactor)
		}
	}

	val, ok = os.LookupEnv("REQ_TIMTEOUT")
	if !ok {
		timeout = 10
	} else {
		timeout, err = strconv.Atoi(val)
		if err != nil {
			panic(err)
		} else {
			log.Infof("Set timeout: %s", timeout)
		}
	}

	username := os.Getenv("REQ_USERNAME")
	password := os.Getenv("REQ_PASSWORD")
	if len(username) > 0 && len(password) > 0 {
		log.Infof("REQ_USERNAME: %s, REQ_PASSWORD: %s", username, password)
	}

	if len(url) == 0 {
		log.Error("No url provided. Doing nothing.")
		return ""
	}

	if method == "GET" || method == "" {
		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		r, _ := ioutil.ReadAll(resp.Body)
		return string(r)
	} else if method == "POST" {
		resp, err := http.Post(url, "application/json; charset=UTF-8", strings.NewReader(payload))
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		r, _ := ioutil.ReadAll(resp.Body)
		return string(r)
	} else {
		log.Error("Invalid REQ_METHOD: '%s', please use 'GET' or 'POST'. Doing nothing.", method)
		return ""
	}
}

func uniqueFilename(filename, namespace, resource, resourceName string) string {
	return "namespace_" + namespace + "." + resource + "_" + resourceName + "." + filename
}

func _get_file_data_and_name(fullFileName, content, resource string) (fileName, fileData string) {
	if strings.HasSuffix(fullFileName, ".url") {
		fileName = fullFileName[:len(fullFileName)-4]
		fileData = request(fileData, "GET", "")
	} else {
		fileName = fullFileName
		fileData = content
	}
	return fileName, fileData
}

func _get_destination_folder(metadata metav1.ObjectMeta, defaultFolder, folderAnnotation string) (destFolder string) {
	if val, ok := metadata.Annotations[folderAnnotation]; ok {
		log.Infof("Found a folder override annotation, placing the %s in: %s", metadata.Name, val)
		return val
	}
	return defaultFolder
}

func configMapsInformer(label, labelValue, targetFolder, url, method, payload, namespace, folderAnnotation string, uniqueFilenames bool) {
	log.Infof("Called configMapsInformer")
	// use the current context in kubeconfig
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	factory := informers.NewSharedInformerFactory(clientset, 0)
	informer := factory.Core().V1().ConfigMaps().Informer()
	stopper := make(chan struct{})
	defer close(stopper)
	defer runtime.HandleCrash()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			mObj := obj.(*corev1.ConfigMap)
			log.Infof("Added configmap %s", mObj.GetName())
			destFolder := _get_destination_folder(mObj.ObjectMeta, targetFolder, folderAnnotation)
			if namespace == "ALL" && processConfigMap(*mObj, destFolder, mObj.ObjectMeta.Namespace, uniqueFilenames, false) && len(url) > 0 {
				request(url, method, payload)
			} else {
				if mObj.ObjectMeta.Namespace == namespace && processConfigMap(*mObj, destFolder, mObj.ObjectMeta.Namespace, uniqueFilenames, false) && len(url) > 0 {
					request(url, method, payload)
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			mObj := obj.(*corev1.ConfigMap)
			log.Infof("Removed configmap %s", mObj.GetName())
			destFolder := _get_destination_folder(mObj.ObjectMeta, targetFolder, folderAnnotation)
			// if processConfigMap(*mObj, destFolder, mObj.ObjectMeta.Namespace, uniqueFilenames, true) && len(url) > 0 {
			// 	request(url, method, payload)
			// }
			if namespace == "ALL" && processConfigMap(*mObj, destFolder, mObj.ObjectMeta.Namespace, uniqueFilenames, true) && len(url) > 0 {
				request(url, method, payload)
			} else {
				if mObj.ObjectMeta.Namespace == namespace && processConfigMap(*mObj, destFolder, mObj.ObjectMeta.Namespace, uniqueFilenames, true) && len(url) > 0 {
					request(url, method, payload)
				}
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			_oldObj := oldObj.(*corev1.ConfigMap)
			_newObj := newObj.(*corev1.ConfigMap)
			if !reflect.DeepEqual(oldObj, newObj) {
				log.Infof("Updated configmap %s", _oldObj.ObjectMeta.Name)
				destFolder := _get_destination_folder(_newObj.ObjectMeta, targetFolder, folderAnnotation)
				if namespace == "ALL" && processConfigMap(*_newObj, destFolder, _newObj.ObjectMeta.Namespace, uniqueFilenames, false) && len(url) > 0 {
					request(url, method, payload)
				} else {
					if _newObj.ObjectMeta.Namespace == namespace && processConfigMap(*_newObj, destFolder, _newObj.ObjectMeta.Namespace, uniqueFilenames, false) && len(url) > 0 {
						request(url, method, payload)
					}
				}
			}
		},
	})

	informer.Run(stopper)
	if !cache.WaitForCacheSync(stopper, informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}
	<-stopper
}

func secretsInformer(label, labelValue, targetFolder, url, method, payload, namespace, folderAnnotation string, uniqueFilenames bool) {
	log.Infof("Called secretsInformer")
	// use the current context in kubeconfig
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	factory := informers.NewSharedInformerFactory(clientset, 0)
	informer := factory.Core().V1().Secrets().Informer()
	stopper := make(chan struct{})
	defer close(stopper)
	defer runtime.HandleCrash()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			mObj := obj.(*corev1.Secret)
			log.Infof("Added secret %s", mObj.GetName())
			destFolder := _get_destination_folder(mObj.ObjectMeta, targetFolder, folderAnnotation)
			if namespace == "ALL" && processSecret(*mObj, destFolder, mObj.ObjectMeta.Namespace, uniqueFilenames, false) && len(url) > 0 {
				request(url, method, payload)
			} else {
				if mObj.ObjectMeta.Namespace == namespace && processSecret(*mObj, destFolder, mObj.ObjectMeta.Namespace, uniqueFilenames, false) && len(url) > 0 {
					request(url, method, payload)
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			mObj := obj.(*corev1.Secret)
			log.Infof("Removing secret %s", mObj.GetName())
			destFolder := _get_destination_folder(mObj.ObjectMeta, targetFolder, folderAnnotation)
			// if processSecret(*mObj, destFolder, mObj.ObjectMeta.Namespace, uniqueFilenames, true) && len(url) > 0 {
			// 	request(url, method, payload)
			// }
			if namespace == "ALL" && processSecret(*mObj, destFolder, mObj.ObjectMeta.Namespace, uniqueFilenames, true) && len(url) > 0 {
				request(url, method, payload)
			} else {
				if mObj.ObjectMeta.Namespace == namespace && processSecret(*mObj, destFolder, mObj.ObjectMeta.Namespace, uniqueFilenames, true) && len(url) > 0 {
					request(url, method, payload)
				}
			}
		},
		UpdateFunc: func(old, new interface{}) {
			oldObj := old.(*corev1.Secret)
			newObj := new.(*corev1.Secret)
			if !reflect.DeepEqual(oldObj, newObj) {
				log.Infof("Updated old configmap %s", oldObj.ObjectMeta.Name)
				destFolder := _get_destination_folder(newObj.ObjectMeta, targetFolder, folderAnnotation)
				if namespace == "ALL" && processSecret(*newObj, destFolder, newObj.ObjectMeta.Namespace, uniqueFilenames, false) && len(url) > 0 {
					request(url, method, payload)
				} else {
					if newObj.ObjectMeta.Namespace == namespace && processSecret(*newObj, destFolder, newObj.ObjectMeta.Namespace, uniqueFilenames, false) && len(url) > 0 {
						request(url, method, payload)
					}
				}
			}
		},
	})

	informer.Run(stopper)
	if !cache.WaitForCacheSync(stopper, informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}
	<-stopper
}

func listResources(label, labelValue, targetFolder, url, method, payload, currentNamespace, folderAnnotation, resource string, uniqueFilenames bool) {
	// use the current context in kubeconfig
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	namespace, ok := os.LookupEnv("NAMESPACE")
	if !ok {
		namespace = currentNamespace
	}

	var labelSelector string
	if len(labelValue) > 0 {
		labelSelector = label + "=" + labelValue
	} else {
		labelSelector = label
	}

	var filesChanged = false
	if resource == "secret" {
		secrets, err := clientset.CoreV1().Secrets(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			panic(err.Error())
		}
		for _, item := range secrets.Items {
			log.Infof("Working on %s: %s/%s", resource, item.ObjectMeta.Namespace, item.ObjectMeta.Name)
			destFolder := _get_destination_folder(item.ObjectMeta, targetFolder, folderAnnotation)
			filesChanged = processSecret(item, destFolder, namespace, uniqueFilenames, false)
		}
	} else {
		configMaps, err := clientset.CoreV1().ConfigMaps(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			panic(err.Error())
		}
		var destFolder string
		for _, item := range configMaps.Items {
			log.Infof("Working on %s: %s/%s", resource, item.ObjectMeta.Namespace, item.ObjectMeta.Name)
			if val, ok := item.ObjectMeta.Annotations[folderAnnotation]; ok {
				destFolder = val
				log.Infof("Found a folder override annotation, placing the %s in: %s", item.ObjectMeta.Name, destFolder)
			}
			if item.Data == nil && item.BinaryData == nil {
				log.Infof("No data/binaryData field in %s", resource)
				continue
			} else if item.Data == nil {
				log.Infof("No data field in %s", resource)
				continue
			}
			destFolder = _get_destination_folder(item.ObjectMeta, targetFolder, folderAnnotation)
			filesChanged = processConfigMap(item, destFolder, namespace, uniqueFilenames, false)
		}
	}

	if len(url) > 0 && filesChanged {
		request(url, method, payload)
	}
}

func watchForChanges(mode, label, labelValue, targetFolder, url, method, payload,
	currentNamespace, folderAnnotation string, resources []string, uniqueFilenames bool) {
}

func main() {
	folderAnnotation, ok := os.LookupEnv("FOLDER_ANNOTATION")
	if !ok {
		log.Infof("No folder annotation was provided, defaulting to k8s-sidecar-target-directory")
		folderAnnotation = "k8s-sidecar-target-directory"
	} else {
		log.Infof("FOLDER_ANNOTATION provided: %s", folderAnnotation)
	}

	label, ok := os.LookupEnv("LABEL")
	if !ok {
		log.Infof("Should have added LABEL as environment variable! Exit")
		os.Exit(-1)
	} else {
		log.Infof("LABEL provided: %s", label)
	}

	labelValue, ok := os.LookupEnv("LABEL_VALUE")
	if ok {
		log.Infof("Filter labels with value: %s", labelValue)
	}

	targetFolder, ok := os.LookupEnv("FOLDER")
	if !ok {
		log.Infof("Shold have added FOLDER as environment variable! Exit")
		os.Exit(-1)
	} else {
		log.Infof("FOLDER provided: %s", targetFolder)
	}

	var resources []string
	resource, ok := os.LookupEnv("RESOURCE")
	if !ok {
		resources = append(resources, "configmap")
	}
	if resource == "both" {
		resources = append(resources, "secret", "configmap")
	}
	log.Infof("Selected resource type: %v", resources)

	method := os.Getenv("REQ_METHOD")
	url := os.Getenv("REQ_URL")
	payload := os.Getenv("REQ_PAYLOAD")

	currentNamespace, _ := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")

	var uniqueFilenames bool
	value, ok := os.LookupEnv("UNIQUE_FILENAMES")
	if ok && value == "true" {
		log.Infof("Unique filenames will be enforced.")
		uniqueFilenames = true
	} else {
		log.Infof("Unique filenames will not be enforced.")
		uniqueFilenames = false
	}

	if os.Getenv("METHOD") == "LIST" {
		for _, res := range resources {
			listResources(label, labelValue, targetFolder, url, method, payload,
				string(currentNamespace), folderAnnotation, res, uniqueFilenames)
		}
	} else {
		go configMapsInformer(label, labelValue, targetFolder, url, method, payload,
			string(currentNamespace), folderAnnotation, uniqueFilenames)
		go secretsInformer(label, labelValue, targetFolder, url, method, payload,
			string(currentNamespace), folderAnnotation, uniqueFilenames)
		for {
			log.Infof("Sleep 5 second")
			time.Sleep(5 * time.Second)
		}
	}
}
