package main

import (
	context "context"
	"fmt"
	"strings"

	"github.com/containerd/containerd/plugin"
	"github.com/containerd/containerd/plugins/test-plugin/api"
	grpc "google.golang.org/grpc"
)

const (
	pluginid = "My Test Plugin"
)

func init() {
	fmt.Println("Starting " + pluginid)
	plugin.Register(&plugin.Registration{
		Type: plugin.GRPCPlugin,
		ID:   "test-plugin",
		Requires: []plugin.Type{
			plugin.ServicePlugin,
		},
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			fmt.Println(pluginid + " : InitFn")
			return &service{}, nil
		},
	})
}

type service struct {
}

func (s *service) Register(server *grpc.Server) error {
	fmt.Println("Registering " + pluginid)
	api.RegisterStringServiceServer(server, s)
	return nil
}

func (s *service) Capitilize(ctx context.Context, r *api.CapRequest) (*api.CapResponse, error) {
	fmt.Println("Starting " + pluginid + " : Get")
	inputStr := r.Input
	fmt.Println("Received input string:", inputStr)
	capitilized := strings.ToUpper(inputStr)
	return &api.CapResponse{Capitilized: capitilized}, nil
}
