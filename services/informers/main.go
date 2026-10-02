package main

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	factory := informers.NewSharedInformerFactory(clientset, 0)
	
	deploymentInformer := factory.Apps().V1().Deployments()

	deploymentLister := deploymentInformer.Lister()


	deploymentInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				deployment := obj.(*appsv1.Deployment)
				fmt.Println("add deployment", deployment.Name)
			},

			UpdateFunc: func(oldObj interface{}, newObj interface{}) {
				oldDeployment := oldObj.(*appsv1.Deployment)
				newDeployment := newObj.(*appsv1.Deployment)
				fmt.Println("update deployment", oldDeployment.Name, newDeployment.Name)
			},

			DeleteFunc: func(obj interface{}) {
				deployment := obj.(*appsv1.Deployment)
				fmt.Println("delete deployment", deployment.Name)
			},
		},
	)				

	stop := make(chan struct{})

	factory.Start(stop)
	

	if !cache.WaitForCacheSync(stop, deploymentInformer.Informer().HasSynced) {
		panic("failed to wait for caches to sync")
	}

	defer func() {
		fmt.Println("stop")
		close(stop)
	}()

	deployments, err := deploymentLister.List(labels.Everything())
	if err != nil {
		panic(err.Error())
	}

	for _, deployment := range deployments {
		fmt.Println("deployment", deployment.Name)
	}

	<-stop
}

