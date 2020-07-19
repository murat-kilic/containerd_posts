package main

import (
	context "context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/containerd/containerd/plugins/test-plugin/api"
	grpc "google.golang.org/grpc"
)

var inputStr []string

func main() {
	if len(os.Args) > 1 {
		inputStr = os.Args[1:]

	} else {
		fmt.Println("USAGE : ./test-plugin.go {string to make uppercase}")
	}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())
	conn, err := grpc.Dial("unix:///run/containerd/containerd.sock", opts...)
	if err != nil {
		fmt.Printf("fail to dial: %v", err)
	}
	defer conn.Close()
	client := api.NewStringServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.Capitilize(ctx, &api.CapRequest{Input: strings.Join(inputStr[:], " ")})
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(resp.Capitilized)

}
