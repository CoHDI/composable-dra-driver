package main

import (
	"cdi_dra/pkg/config"
	"cdi_dra/pkg/manager"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/urfave/cli/v2"
)

const (
	uuidFormat = "^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$"
)

func main() {
	if err := newApp().Run(os.Args); err != nil {
		slog.Error("Command Failed", "error", err)
		os.Exit(1)
	}
}

func newApp() *cli.App {
	config := &config.Config{}
	cliFlags := []cli.Flag{
		&cli.IntFlag{
			Name:        "v",
			Usage:       "Set the log level, CDI_DRA will only log message whose level is higher than this value. Default is 0.\n CDI_DRA logs error at level 8, logs warning at level 4, logs info at level 0 and logs debug at level -4. \n If log level is set larger than 8, CDI_DRA will not log any messages.",
			Destination: &config.LogLevel,
		},
		&cli.DurationFlag{
			Name:        "scan-interval",
			Usage:       "How often CDI resource pool is checked for renewing ResourceSlice. Its format can be set as XXhYYmZZs.",
			Destination: &config.ScanInterval,
			EnvVars:     []string{"SCAN_INTERVAL"},
			Value:       1 * time.Minute,
			Action: func(ctx *cli.Context, scanInterval time.Duration) error {
				if scanInterval <= 0*time.Second || 86400*time.Second <= scanInterval {
					return fmt.Errorf("scan interval must be set from 0s to 86400s")
				}
				return nil
			},
		},
		&cli.StringFlag{
			Name:        "tenant-id",
			Usage:       "ID of tenant where a cluster belongs. Must specify a form of UUID",
			Required:    true,
			Destination: &config.TenantID,
			EnvVars:     []string{"TENANT_ID"},
			Action: func(ctx *cli.Context, tenantId string) error {
				r := regexp.MustCompile(uuidFormat)
				if !r.MatchString(tenantId) {
					return fmt.Errorf("tenant id must be set as uuid format")
				}
				return nil
			},
		},
		&cli.StringFlag{
			Name:        "cluster-id",
			Usage:       "ID of cluster where CDI_DRA is executed. Must specify a form of UUID",
			Required:    true,
			Destination: &config.ClusterID,
			EnvVars:     []string{"CLUSTER_ID"},
			Action: func(ctx *cli.Context, clusterId string) error {
				r := regexp.MustCompile(uuidFormat)
				if !r.MatchString(clusterId) {
					return fmt.Errorf("cluster id must be set as uuid format")
				}
				return nil
			},
		},
		&cli.StringFlag{
			Name:        "cdi-endpoint",
			Usage:       "Endpoint of CDI API server. Must specify host name where working CDI manager",
			Required:    true,
			Destination: &config.CDIEndpoint,
			EnvVars:     []string{"CDI_ENDPOINT"},
			Action: func(ctx *cli.Context, endpoint string) error {
				if len(endpoint) > 1000 {
					return fmt.Errorf("cdi endpoint length must be set within 1000 bytes")
				}
				if !strings.HasPrefix(endpoint, "https://") {
					return fmt.Errorf("cdi endpoint format must be set starting https://")
				} else {
					config.CDIEndpoint = strings.TrimPrefix(endpoint, "https://")
				}
				return nil
			},
		},
		&cli.BoolFlag{
			Name:        "use-capi-bmh",
			Usage:       "Whether to use cluster-api and BareMetalHost or not to get machine uuid",
			Destination: &config.UseCapiBmh,
			EnvVars:     []string{"USE_CAPI_BMH"},
			Value:       false,
		},
		&cli.Int64Flag{
			Name:    "binding-timeout",
			Usage:   "Timeout in seconds (default: 600) for BindingTimeoutSeconds in ResourceSlice when enable DRADeviceBindingConditions. It must be set from 0 to 86400",
			EnvVars: []string{"BINDING_TIMEOUT_SEC"},
			Action: func(ctx *cli.Context, timeout int64) error {
				if timeout <= 0 || 86400 <= timeout {
					return fmt.Errorf("binding timeout must be set from 0 to 86400")
				} else {
					config.BindingTimout = &timeout
				}
				return nil
			},
		},
	}

	app := &cli.App{
		Name:            "cdi-dra",
		Usage:           "cdi-dra implements a DRA driver for CDI fabric devices",
		HideHelpCommand: true,
		Flags:           cliFlags,
		Before: func(c *cli.Context) error {
			if c.Args().Len() > 0 {
				return fmt.Errorf("arguments not supported: %v", c.Args().Slice())
			}
			return nil
		},
		Action: func(c *cli.Context) error {
			opts := &slog.HandlerOptions{
				AddSource:   true,
				Level:       slog.Level(config.LogLevel),
				ReplaceAttr: replaceAttr,
			}
			logger := slog.New(slog.NewTextHandler(os.Stdout, opts)).With("compo", "CDI_DRA")
			slog.SetDefault(logger)

			slog.Info("CDI_DRA start")

			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
			ctx, cancel := context.WithCancel(c.Context)
			ctx = logr.NewContextWithSlogLogger(ctx, logger)
			defer func() {
				cancel()
			}()

			errChan := make(chan error, 1)
			go func() {
				errChan <- manager.StartCDIManager(ctx, config)
			}()

			select {
			case s := <-sigs:
				slog.Info("Signal received", "signal", s.String())
				return nil
			case err := <-errChan:
				slog.Error("Failed start manager", "error", err)
				return err
			}
		},
	}
	return app
}

func replaceAttr(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.SourceKey {
		_, file, line, ok := runtime.Caller(6)
		if !ok {
			return attr
		}
		v := fmt.Sprintf("%s:%d", filepath.Base(file), line)
		attr.Value = slog.StringValue(v)
	}
	return attr
}
