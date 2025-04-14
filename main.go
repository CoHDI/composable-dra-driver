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
	"runtime"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/urfave/cli/v2"
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
			Usage:       "How often CDI resource pool is checked for renewing ResourceSlice",
			Destination: &config.ScanInterval,
			EnvVars:     []string{"SCAN_INTERVAL"},
			Value:       1 * time.Minute,
		},
		&cli.StringFlag{
			Name:        "tenant-id",
			Usage:       "ID of tenant you belong. Must specify a form of UUID",
			Destination: &config.TenantID,
			EnvVars:     []string{"TENANT_ID"},
		},
		&cli.StringFlag{
			Name:        "cdi-endpoint",
			Usage:       "Endpoint of CDI API server. Must specify host name where working CDI manager",
			Required:    true,
			Destination: &config.CDIEndpoint,
			EnvVars:     []string{"CDI_ENDPOINT"},
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
