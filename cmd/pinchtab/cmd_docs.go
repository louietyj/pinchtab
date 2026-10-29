package main

import (
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/pinchtab/pinchtab/skills"
	"github.com/spf13/cobra"
)

var docsCmd = &cobra.Command{
	Use:   "docs [reference]",
	Short: "Print the agent skill for this build, or one of its references (commands, api, ...)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "pinchtab/SKILL.md"
		if len(args) == 1 {
			path = "pinchtab/references/" + strings.TrimSuffix(args[0], ".md") + ".md"
		}
		data, err := fs.ReadFile(skills.Pinchtab, path)
		if err != nil {
			entries, _ := fs.ReadDir(skills.Pinchtab, "pinchtab/references")
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, strings.TrimSuffix(e.Name(), ".md"))
			}
			return fmt.Errorf("no reference %q; there are: %s", args[0], strings.Join(names, ", "))
		}
		_, err = os.Stdout.Write(data)
		return err
	},
}

func init() {
	rootCmd.AddCommand(docsCmd)
}
