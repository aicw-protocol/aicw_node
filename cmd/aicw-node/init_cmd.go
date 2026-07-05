package main

import (
	"context"
	"fmt"

	"github.com/aicw/aicw_node/pkg/identity"
	"github.com/urfave/cli/v3"
)

func runInit(ctx context.Context, c *cli.Command) error {
	nodeName := c.String("name")
	outputDir := c.String("output-dir")
	overwrite := c.Bool("overwrite")

	result, err := identity.GenerateNodeIdentity(identity.GenerateOptions{
		NodeName:  nodeName,
		OutputDir: outputDir,
		Overwrite: overwrite,
	})
	if err != nil {
		return err
	}

	fmt.Println("Identity created successfully.")
	fmt.Println()
	fmt.Println("Give these values to your network administrator for whitelist registration:")
	fmt.Printf("  node_name:  %s\n", result.NodeName)
	fmt.Printf("  node_id:    %s\n", result.NodeID)
	fmt.Printf("  public_key: %s\n", result.PublicKey)
	fmt.Println()
	fmt.Println("Files written:")
	fmt.Printf("  %s\n", result.IdentityPath)
	fmt.Printf("  %s\n", result.PrivateKeyPath)
	return nil
}
