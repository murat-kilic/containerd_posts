package main

import (
	context "context"
	"io/ioutil"
	"os"
	"strings"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/api/services/containers/v1"
	"github.com/containerd/containerd/api/services/diff/v1"
	"github.com/containerd/containerd/api/services/images/v1"
	"github.com/containerd/containerd/api/services/namespaces/v1"
	"github.com/containerd/containerd/api/services/tasks/v1"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/leases"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/plugin"
	"github.com/containerd/containerd/plugins/test-plugin2/api"
	"github.com/containerd/containerd/services"
	"github.com/containerd/containerd/snapshots"
	"github.com/pkg/errors"
	grpc "google.golang.org/grpc"
)

const (
	pluginName = "Container Ops Plugin Using Containerd Services"
	logFile    = "/home/centos/go/src/github.com/containerd/containerd/plugins/test-plugin2/logs/ContainerOpsPlugin.log"
)

func init() {
	log.G(context.Background()).Infof("%s : Init", pluginName)

	plugin.Register(&plugin.Registration{
		Type: plugin.GRPCPlugin,
		ID:   "test-plugin",
		Requires: []plugin.Type{
			plugin.ServicePlugin,
		},
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			log.G(context.Background()).Infof("%s : InitFn", pluginName)

			servicesOpts, err := getServicesOpts(ic)
			if err != nil {
				log.G(context.Background()).Error("failed to get services")
				return nil, err
			}

			client, err := containerd.New(
				"",
				containerd.WithServices(servicesOpts...),
			)
			if err != nil {
				log.G(context.Background()).Error("Failed to create containerd client")
				return nil, err
			}

			return &containerOpsService{client: client}, nil
		},
	})
}

type containerOpsService struct {
	client *containerd.Client
}

func (s *containerOpsService) Register(srvr *grpc.Server) error {
	log.G(context.Background()).Infof("%s : Register", pluginName)
	api.RegisterContainerOpsServiceServer(srvr, s)
	return nil
}

func (s *containerOpsService) Run(ctx context.Context, r *api.RunRequest) (*api.RunResponse, error) {

	// Check imageName and containerName provided
	imageName := r.ImageName
	containerName := r.ContainerName
	if imageName == "" || containerName == "" {
		return nil, errors.New("You must provide an image name and container name")
	}

	// Remove Log File First
	os.Remove(logFile)

	// Get Image or if it does not exist Pull it

	log.G(ctx).WithField("Image Name", imageName).WithField("Container Name", containerName).WithField("Command", r.Cmd).Infof("%s : Run", pluginName)
	image, err := s.client.GetImage(ctx, imageName)
	if err != nil {
		image, err = s.client.Pull(ctx, imageName, containerd.WithPullUnpack)
		if err != nil {
			log.G(ctx).Errorf("couldn't pull image %s: $v", imageName, err)
			return nil, err
		}
	}

	// Create Container
	var container containerd.Container
	var cmd []string
	if r.Cmd == "" {
		cmd = nil
	} else {
		cmd = strings.Split(r.Cmd, " ")
	}
	container, err = s.client.NewContainer(ctx, r.ContainerName,
		containerd.WithNewSnapshot(r.ContainerName, image),
		containerd.WithNewSpec(oci.WithImageConfigArgs(image, cmd)))
	if err != nil {
		log.G(ctx).Errorf("error creating container:%v", err)
		return nil, err
	}

	// Create Task
	task, err := container.NewTask(ctx, cio.LogFile(logFile))

	if err != nil {
		log.G(ctx).Errorf("error creating task: %v", err)
		return nil, err
	}

	// Wait on task
	statusC, err := task.Wait(ctx)
	if err != nil {
		log.G(ctx).Error("error waiting on task\n")
		return nil, err
	}

	// Start the task
	if err := task.Start(ctx); err != nil {
		task.Delete(ctx)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		log.G(ctx).Errorf("error starting task: %v", err)
		return nil, err
	}
	_ = <-statusC
	task.Delete(ctx)
	container.Delete(ctx, containerd.WithSnapshotCleanup)

	content, _ := ioutil.ReadFile(logFile)
	return &api.RunResponse{Output: string(content)}, nil
}

func getServicesOpts(ic *plugin.InitContext) ([]containerd.ServicesOpt, error) {
	plugins, err := ic.GetByType(plugin.ServicePlugin)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get service plugin")
	}

	opts := []containerd.ServicesOpt{
		containerd.WithEventService(ic.Events),
	}
	for s, fn := range map[string]func(interface{}) containerd.ServicesOpt{
		services.ContentService: func(s interface{}) containerd.ServicesOpt {
			return containerd.WithContentStore(s.(content.Store))
		},
		services.ImagesService: func(s interface{}) containerd.ServicesOpt {
			return containerd.WithImageService(s.(images.ImagesClient))
		},
		services.SnapshotsService: func(s interface{}) containerd.ServicesOpt {
			return containerd.WithSnapshotters(s.(map[string]snapshots.Snapshotter))
		},
		services.ContainersService: func(s interface{}) containerd.ServicesOpt {
			return containerd.WithContainerService(s.(containers.ContainersClient))
		},
		services.TasksService: func(s interface{}) containerd.ServicesOpt {
			return containerd.WithTaskService(s.(tasks.TasksClient))
		},
		services.DiffService: func(s interface{}) containerd.ServicesOpt {
			return containerd.WithDiffService(s.(diff.DiffClient))
		},
		services.NamespacesService: func(s interface{}) containerd.ServicesOpt {
			return containerd.WithNamespaceService(s.(namespaces.NamespacesClient))
		},
		services.LeasesService: func(s interface{}) containerd.ServicesOpt {
			return containerd.WithLeasesService(s.(leases.Manager))
		},
	} {
		p := plugins[s]
		if p == nil {
			return nil, errors.Errorf("service %q not found", s)
		}
		i, err := p.Instance()
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get instance of service %q", s)
		}
		if i == nil {
			return nil, errors.Errorf("instance of service %q not found", s)
		}
		opts = append(opts, fn(i))
	}
	return opts, nil
}
