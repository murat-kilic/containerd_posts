package main

import (
	context "context"
	"fmt"
	"os"
	"time"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/plugins/test-plugin3/api"
	grpc "google.golang.org/grpc"
)

var (
	fileLocation, applicationName string
)

func main() {
	if len(os.Args) == 3 {
		applicationName = os.Args[1]
		fileLocation = os.Args[2]
		
	} else {
		fmt.Println("USAGE : ./test-plugin3-client applicationName fileLocation")
		return
	}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	conn, err := grpc.Dial("unix:///run/containerd/containerd.sock", opts...)
	if err != nil {
		fmt.Printf("fail to dial: %v", err)
	}
	defer conn.Close()

	client := api.NewJavaRuntimeServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = namespaces.WithNamespace(ctx, "default")
	resp, err := client.CreateContainer(ctx, &api.CreateContainerRequest{ApplicationName: applicationName, FileLocation:fileLocation})
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(resp.ContainerId)

}