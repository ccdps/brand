// Command brandkit generates and checks the CCDPS brand kit in kit/.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ccdps/brand/brandkit"
)

var osExit = os.Exit

func main() {
	if err := RootCmd().Execute(); err != nil {
		osExit(1)
	}
}

type srcOpts struct{ mark, font string }

func (o *srcOpts) flags(c *cobra.Command) {
	c.Flags().StringVar(&o.mark, "mark", "assets/ccdps-mark-v3.svg", "pictogram SVG (source of truth)")
	c.Flags().StringVar(&o.font, "font", "fonts/Figtree-700.ttf", "Figtree 700 TTF (OFL)")
}

func (o srcOpts) kit() (*brandkit.Kit, error) {
	svg, err := os.ReadFile(o.mark)
	if err != nil {
		return nil, err
	}
	ttf, err := os.ReadFile(o.font)
	if err != nil {
		return nil, err
	}
	return brandkit.New(string(svg), ttf)
}

// RootCmd builds the command tree.
func RootCmd() *cobra.Command {
	root := &cobra.Command{Use: "brandkit", Short: "generate and check the CCDPS brand kit", SilenceUsage: true}

	var gen srcOpts
	var out string
	g := &cobra.Command{Use: "generate", Short: "write every kit file into --out", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			k, err := gen.kit()
			if err != nil {
				return err
			}
			names, err := k.Generate(out)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %d files into %s\n", len(names), out)
			return nil
		}}
	gen.flags(g)
	g.Flags().StringVar(&out, "out", "kit", "output directory")
	root.AddCommand(g)

	var chk srcOpts
	var dir, against string
	c := &cobra.Command{Use: "check", Short: "verify the kit in --dir (and, with --against, that it equals a fresh generation)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			k, err := chk.kit()
			if err != nil {
				return err
			}
			bad := k.Check(dir, against)
			for _, b := range bad {
				fmt.Fprintln(cmd.ErrOrStderr(), b)
			}
			if len(bad) > 0 {
				return fmt.Errorf("%d problem(s) in %s", len(bad), dir)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "kit %s ok\n", dir)
			return nil
		}}
	chk.flags(c)
	c.Flags().StringVar(&dir, "dir", "kit", "kit directory to check")
	c.Flags().StringVar(&against, "against", "", "directory holding a fresh `brandkit generate` to compare with")
	root.AddCommand(c)
	return root
}
