package cli

import (
	"errors"
	"io"
	"log/slog"

	"github.com/edocsss/agent-whiteboard/pkg/agentwb"
	"github.com/spf13/cobra"
)

type applicationArguments struct {
	config            agentwb.Config
	port              int
	defaultExpiration int64
}

func buildApplicationArguments(settings resolvedServerSettings, logWriter io.Writer) applicationArguments {
	var handler slog.Handler
	if settings.logMode == string(agentwb.LogModeJSON) {
		handler = slog.NewJSONHandler(logWriter, nil)
	} else {
		handler = slog.NewTextHandler(logWriter, nil)
	}
	return applicationArguments{
		config: agentwb.Config{
			RootDir:              settings.storage,
			CleanupInterval:      settings.cleanupInterval,
			Host:                 settings.host,
			ShutdownTimeout:      settings.shutdownTimeout,
			MaxWhiteboardBytes:   settings.maxWhiteboardBytes,
			MaxImageBytes:        settings.maxImageBytes,
			MaxImageRequestBytes: settings.maxImageRequestBytes,
			LogMode:              agentwb.LogMode(settings.logMode),
			Logger:               slog.New(handler),
		},
		port:              settings.port,
		defaultExpiration: settings.defaultExpiration,
	}
}

func (arguments applicationArguments) options() []agentwb.Option {
	return []agentwb.Option{
		agentwb.WithPort(arguments.port),
		agentwb.WithDefaultExpiration(arguments.defaultExpiration),
	}
}

func (factory commandFactory) newServeCommand() *cobra.Command {
	values := &serverFlagValues{}
	command := &cobra.Command{
		Use:  "serve",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return factory.runServe(cmd, values)
		},
	}
	flags := command.Flags()
	flags.StringVar(&values.host, "host", "", "bind host")
	flags.StringVar(&values.port, "port", "", "bind port")
	flags.StringVar(&values.storage, "storage", "", "storage root")
	flags.StringVar(&values.cleanupInterval, "cleanup-interval", "", "cleanup interval")
	flags.StringVar(&values.defaultExpiration, "default-expires-in", "", "default expiration in seconds")
	flags.StringVar(&values.shutdownTimeout, "shutdown-timeout", "", "shutdown timeout")
	flags.StringVar(&values.logMode, "log-mode", "", "console or json logging")
	flags.StringVar(&values.maxWhiteboardBytes, "max-whiteboard-bytes", "", "maximum whiteboard size")
	flags.StringVar(&values.maxImageBytes, "max-image-bytes", "", "maximum image size")
	flags.StringVar(&values.maxImageRequestBytes, "max-image-request-bytes", "", "maximum image request size")
	return command
}

func (factory commandFactory) runServe(cmd *cobra.Command, values *serverFlagValues) (resultErr error) {
	settings, err := factory.resolveServerSettings(cmd, values)
	if err != nil {
		return err
	}
	arguments := buildApplicationArguments(settings, factory.deps.Stderr)
	application, err := factory.deps.NewApplication(arguments.config, arguments.options()...)
	if err != nil {
		return err
	}
	if isNilLike(application) {
		return errors.New("application factory returned nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, application.Close())
	}()

	resultErr = application.ListenAndServe(cmd.Context())
	if contextErr := cmd.Context().Err(); contextErr != nil {
		resultContext, contextOnly := contextOnlyError(resultErr)
		if resultErr == nil || (contextOnly && resultContext == contextErr) {
			resultErr = nil
		}
	}
	return resultErr
}
