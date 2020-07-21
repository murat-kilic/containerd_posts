package main

import (
	context "context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/plugins/test-plugin2/api"
	grpc "google.golang.org/grpc"
)

var (
	imageName, containerName, cmd string
)

func main() {
	if len(os.Args) > 2 {
		imageName = os.Args[1]
		containerName = os.Args[2]
		if len(os.Args) > 3 {
			cmd = strings.Join(os.Args[3:], " ")
		} else {
			cmd = ""
		}

	} else {
		fmt.Println("USAGE : ./test-plugin2-client ImageName ContainerName [Command]")
		return
	}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	conn, err := grpc.Dial("unix:///run/containerd/containerd.sock", opts...)
	if err != nil {
		fmt.Printf("fail to dial: %v", err)
	}
	defer conn.Close()

	client := api.NewContainerOpsServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = namespaces.WithNamespace(ctx, "default")
	resp, err := client.Run(ctx, &api.RunRequest{ImageName: imageName, ContainerName: containerName, Cmd: cmd})
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(resp.Output)

}
