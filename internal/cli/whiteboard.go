package cli

import (
	"github.com/edocsss/agent-whiteboard/internal/http"
	"github.com/spf13/cobra"
)

func (factory commandFactory) newCreateCommand() *cobra.Command {
	command := &cobra.Command{Use: "create", Args: cobra.NoArgs}
	command.AddCommand(factory.newCreateWhiteboardCommand("markdown", http.WhiteboardMarkdown))
	command.AddCommand(factory.newCreateWhiteboardCommand("html", http.WhiteboardHTML))
	return command
}

func (factory commandFactory) newCreateWhiteboardCommand(name string, kind http.WhiteboardKind) *cobra.Command {
	command := &cobra.Command{Use: name + " <file>", Args: cobra.ExactArgs(1)}
	expires := expirationFlag(command)
	command.RunE = func(cmd *cobra.Command, args []string) error {
		expiration, err := resolveExpiration(cmd, *expires)
		if err != nil {
			return err
		}
		opened, file, err := openRegularFile(args[0])
		if err != nil {
			return err
		}
		defer opened.Close()

		client, ctx, cancel, err := factory.newClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		created, err := client.CreateWhiteboard(ctx, kind, file, expiration)
		if err != nil {
			return stableCommandError(err)
		}
		return stableCommandError(writeResource(factory.deps.Stdout, factory.root.json, client, created))
	}
	return command
}

func (factory commandFactory) newUpdateCommand() *cobra.Command {
	command := &cobra.Command{Use: "update", Args: cobra.NoArgs}
	command.AddCommand(factory.newUpdateWhiteboardCommand("markdown", http.WhiteboardMarkdown))
	command.AddCommand(factory.newUpdateWhiteboardCommand("html", http.WhiteboardHTML))
	return command
}

func (factory commandFactory) newUpdateWhiteboardCommand(name string, kind http.WhiteboardKind) *cobra.Command {
	command := &cobra.Command{Use: name + " <id> <file>", Args: cobra.ExactArgs(2)}
	expires := expirationFlag(command)
	command.RunE = func(cmd *cobra.Command, args []string) error {
		expiration, err := resolveExpiration(cmd, *expires)
		if err != nil {
			return err
		}
		opened, file, err := openRegularFile(args[1])
		if err != nil {
			return err
		}
		defer opened.Close()

		client, ctx, cancel, err := factory.newClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		updated, err := client.UpdateWhiteboard(ctx, kind, args[0], file, expiration)
		if err != nil {
			return stableCommandError(err)
		}
		return stableCommandError(writeResource(factory.deps.Stdout, factory.root.json, client, updated))
	}
	return command
}

func (factory commandFactory) newDeleteCommand() *cobra.Command {
	command := &cobra.Command{Use: "delete", Args: cobra.NoArgs}
	command.AddCommand(factory.newDeleteWhiteboardCommand("markdown", http.WhiteboardMarkdown))
	command.AddCommand(factory.newDeleteWhiteboardCommand("html", http.WhiteboardHTML))
	return command
}

func (factory commandFactory) newDeleteWhiteboardCommand(name string, kind http.WhiteboardKind) *cobra.Command {
	return &cobra.Command{
		Use:  name + " <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, cancel, err := factory.newClient(cmd)
			if err != nil {
				return err
			}
			defer cancel()
			if err := client.DeleteWhiteboard(ctx, kind, args[0]); err != nil {
				return stableCommandError(err)
			}
			return writeDeleteSuccess(factory.deps.Stdout, factory.root.json)
		},
	}
}
