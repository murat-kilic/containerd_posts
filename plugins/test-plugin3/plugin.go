package main

import (
	context "context"
	"io/ioutil"
	"os"
	"strings"
	"time"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/api/services/containers/v1"
	"github.com/containerd/containerd/api/services/diff/v1"
	"github.com/containerd/containerd/api/services/images/v1"
	"github.com/containerd/containerd/api/services/namespaces/v1"
	"github.com/containerd/containerd/api/services/tasks/v1"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/leases"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/plugin"
	"github.com/containerd/containerd/plugins/test-plugin3/api"
	"github.com/containerd/containerd/services"
	"github.com/containerd/containerd/snapshots"
	"github.com/pkg/errors"
	"github.com/google/uuid"
	"github.com/opencontainers/image-spec/identity"
	grpc "google.golang.org/grpc"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	pluginName = "Java Container Plugin Using Containerd Services"
	logFile    = "/home/centos/go/src/github.com/containerd/containerd/plugins/test-plugin3/logs/JavaContainerPlugin.log"
)

func init() {
	log.G(context.Background()).Infof("%s : Init", pluginName)

	plugin.Register(&plugin.Registration{
		Type: plugin.GRPCPlugin,
		ID:   "java-container",
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

			return &javaRuntimeService{client: client}, nil
		},
	})
}

type javaRuntimeService struct {
	client *containerd.Client
}

func (s *javaRuntimeService) Register(srvr *grpc.Server) error {
	log.G(context.Background()).Infof("%s : Register", pluginName)
	api.RegisterJavaRuntimeServiceServer(srvr, s)
	return nil
}

func (s *javaRuntimeService) CreateContainer(ctx context.Context, r *api.CreateContainerRequest) (*api.CreateContainerResponse, error) {

	// Check applicationName and fileLocation provided
	appName := r.ApplicationName
	appFileLocation := r.FileLocation
	//containerName := r.ContainerName
	if appName == "" || appFileLocation == "" {
		return nil, errors.New("You must provide an application name and file location")
	}

	containerId := uuid.New().String()
	log.G(ctx).Printf("container id: %s\n", containerId)

	opts := []containerd.NewContainerOpts{}
	
	// Set App Name as label of container
	containerLabels := map[string]string{
		"appName": appName,
		}
	opts = append(opts,containerd.WithContainerLabels(containerLabels))
	
	ctx, done, err := s.client.WithLease(ctx)
			if err != nil {
			return nil, err
			}
	defer done(ctx)

	imageName:="docker.io/library/openjdk:8-jre-alpine"

	image, err := s.client.GetImage(ctx, imageName)
	if err != nil {
		image, err = s.client.Pull(ctx, imageName, containerd.WithPullUnpack)
		if err != nil {
			log.G(ctx).Errorf("couldn't pull image %s: $v", imageName, err)
			return nil,err
		}
	}

		err = s.prepareSnapshot(ctx, containerId, appFileLocation, image)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to prepare snapshot . %q", err)
		}
		opts = append(opts, containerd.WithSnapshot(containerId))


		// We need to tell container to start the springboot app
		dirArray := strings.Split(appFileLocation, "/")
		jarFileName := dirArray[len(dirArray)-1]
	
	// Add all spec opts
	opts = append(opts,containerd.WithNewSpec(
		oci.WithImageConfig(image),
		oci.WithProcessArgs("java", "-jar", "/app/"+jarFileName),
		oci.WithHostNamespace(specs.NetworkNamespace),
		oci.WithHostHostsFile,
		oci.WithHostResolvconf))

	var cntr containerd.Container
	if cntr, err = s.client.NewContainer(ctx, containerId, opts...); err != nil {
		return nil, errors.Wrap(err, "failed to create containerd container")
	}
	defer func() {
		if err != nil {
			deferCtx, deferCancel := context.WithTimeout(ctx, 1 * time.Minute)
			defer deferCancel()
			if err := cntr.Delete(deferCtx, containerd.WithSnapshotCleanup); err != nil {
				log.G(ctx).WithError(err).Errorf("Failed to delete containerd container %q", containerId)
			}
		}
	}()

	return &api.CreateContainerResponse{ContainerId:containerId}, nil
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


func (s *javaRuntimeService) prepareSnapshot(ctx context.Context, containerId string, appFileLocation string, image containerd.Image) error {
	 mountTarget := "/tmp/"+containerId

	// Check if dir to mount snapshot exists
   	os.Mkdir(mountTarget, 0755)
	// Delete at the end
	defer func() {
		if err := os.Remove(mountTarget); err != nil {
			log.G(ctx).Error("failed to cleanup snapshot mount dir",err)
		}
	}()

	appDir:="/app/"
	
	diffIDs, err := image.RootFS(ctx)
	if err != nil {
		return errors.Wrapf(err, " could not get diffIDs")
	}
	parent := identity.ChainID(diffIDs).String()

	snapshotMounts, err := s.client.SnapshotService("overlayfs").Prepare(ctx, containerId, parent)

	if err := mount.All(snapshotMounts, mountTarget); err != nil {
		return err
	}

	defer func() {
		if err := mount.Unmount(mountTarget, 0); err != nil {
			log.G(ctx).Error("error Unmounting snapshot",err)
		}
	}()

	// Empty app dir
	appFullDir :=mountTarget+appDir
    e := os.RemoveAll(appFullDir) 
    if e == nil { 
			e=os.Mkdir(appFullDir, 0755) 
	} 
	
	//Copy app file to snapshot mount
	var (
		appFileName string
	)

		dirArray := strings.Split(appFileLocation, "/")
		appFileName = dirArray[len(dirArray)-1]
		log.G(ctx).Debugf("App File Name : %s", appFileName)

		input, err := ioutil.ReadFile(appFileLocation)
		if err != nil {
			return errors.Wrapf(err, "Failed reading App file "+appFileLocation)
		}
		err = ioutil.WriteFile(mountTarget+appDir+appFileName, input, 0644)
		if err != nil {
			return errors.Wrapf(err, "Failed copying app file "+appFileLocation)
		}

	return nil
}