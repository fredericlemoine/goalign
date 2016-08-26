package cmd

import (
	"github.com/fredericlemoine/goalign/io"
	"github.com/spf13/cobra"
)

var trimFromStart bool

// seqCmd represents the seq command
var seqCmd = &cobra.Command{
	Use:   "seq",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := rootalign.TrimSequences(trimNb, trimFromStart); err != nil {
			io.ExitWithMessage(err)
		} else {
			writeAlign(rootalign, trimAlignOut)
		}
	},
}

func init() {
	trimCmd.AddCommand(seqCmd)
	seqCmd.PersistentFlags().IntVarP(&trimNb, "nb-char", "n", 1, "Number of characters to trim from sequences")
	seqCmd.PersistentFlags().BoolVarP(&trimFromStart, "from-start", "s", false, "If true: trims n char from start, else from end")
}
