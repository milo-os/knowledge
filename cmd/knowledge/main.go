package main

import (
	"os"

	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"

	apiserverapp "go.miloapis.com/knowledge/cmd/knowledge/apiserver"
	controllermanagerapp "go.miloapis.com/knowledge/cmd/knowledge/controller-manager"
)

func main() {
	root := &cobra.Command{
		Use:   "knowledge",
		Short: "Knowledge Graph Service for Milo OS",
		Long:  "knowledge manages typed resource relationships for Kubernetes-native platforms.",
	}
	root.AddCommand(apiserverapp.NewCommand())
	root.AddCommand(controllermanagerapp.NewCommand())
	os.Exit(cli.Run(root))
}
