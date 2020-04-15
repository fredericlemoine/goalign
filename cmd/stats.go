package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/evolbioinfo/goalign/align"
	"github.com/evolbioinfo/goalign/io"
	"github.com/spf13/cobra"
)

// statsCmd represents the stats command
var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Prints different characteristics of the alignment",
	Long: `Prints different characteristics of the alignment.

1. Length of alignment;
2. Number of sequences;
3. Average number of alleles per site;
4. Number of variables sites (does ot take into account gaps or special characters);
5. Character frequencies.

If the input alignment contains several alignments, will process all of them

`,
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		var aligns *align.AlignChannel

		if aligns, err = readalign(infile); err != nil {
			io.LogError(err)
			return
		}
		for al := range aligns.Achan {
			fmt.Fprintf(os.Stdout, "length\t%d\n", al.Length())
			fmt.Fprintf(os.Stdout, "nseqs\t%d\n", al.NbSequences())
			fmt.Fprintf(os.Stdout, "avgalleles\t%.4f\n", al.AvgAllelesPerSite())
			fmt.Fprintf(os.Stdout, "variable sites\t%d\n", al.NbVariableSites())
			printCharStats(al, "*")
			fmt.Fprintf(os.Stdout, "alphabet\t%s\n", al.AlphabetStr())
		}

		if aligns.Err != nil {
			err = aligns.Err
			io.LogError(err)
		}
		return
	},
}

func printCharStats(align align.Alignment, only string) {
	charmap := align.CharStats()

	// We add the only character we want to output
	// To write 0 if there are no occurences of it
	// in the alignment
	if _, ok := charmap[rune(only[0])]; !ok && only != "*" {
		charmap[rune(only[0])] = 0
	}

	keys := make([]string, 0, len(charmap))
	var total int64 = 0
	for k, v := range charmap {
		if only == "*" || string(k) == only {
			keys = append(keys, string(k))
		}
		total += v
	}
	sort.Strings(keys)

	fmt.Fprintf(os.Stdout, "char\tnb\tfreq\n")
	for _, k := range keys {
		nb := charmap[rune(k[0])]
		fmt.Fprintf(os.Stdout, "%s\t%d\t%f\n", k, nb, float64(nb)/float64(total))
	}
}

func printSiteCharStats(align align.Alignment, only string) (err error) {
	var sitemap map[rune]int

	charmap := align.CharStats()

	// We add the only character we want to output
	// To write 0 if there are no occurences of it
	// in the alignment
	if _, ok := charmap[rune(only[0])]; !ok && only != "*" {
		charmap[rune(only[0])] = 0
	}

	keys := make([]string, 0, len(charmap))
	for k := range charmap {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	fmt.Fprintf(os.Stdout, "site")
	for _, v := range keys {
		if only == "*" || v == only {
			fmt.Fprintf(os.Stdout, "\t%s", v)
		}
	}
	fmt.Fprintf(os.Stdout, "\n")
	for site := 0; site < align.Length(); site++ {
		if sitemap, err = align.CharStatsSite(site); err != nil {
			return
		}
		fmt.Fprintf(os.Stdout, "%d", site)
		for _, k := range keys {
			if only == "*" || k == only {
				nb := sitemap[rune(k[0])]
				fmt.Fprintf(os.Stdout, "\t%d", nb)
			}
		}
		fmt.Fprintf(os.Stdout, "\n")
	}
	return
}

func printSequenceCharStats(sb align.SeqBag, only string) (err error) {
	var sequencemap map[rune]int

	charmap := sb.CharStats()

	// We add the only character we want to output
	// To write 0 if there are no occurences of it
	// in the alignment
	if _, ok := charmap[rune(only[0])]; !ok && only != "*" {
		charmap[rune(only[0])] = 0
	}

	keys := make([]string, 0, len(charmap))
	for k := range charmap {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	fmt.Fprintf(os.Stdout, "seq")
	for _, v := range keys {
		if only == "*" || v == only {
			fmt.Fprintf(os.Stdout, "\t%s", v)
		}
	}
	fmt.Fprintf(os.Stdout, "\n")
	for i := 0; i < sb.NbSequences(); i++ {
		if sequencemap, err = sb.CharStatsSeq(i); err != nil {
			return
		}
		name, _ := sb.GetSequenceNameById(i)
		fmt.Fprintf(os.Stdout, "%s", name)
		for _, k := range keys {
			if only == "*" || k == only {
				nb := sequencemap[rune(k[0])]
				fmt.Fprintf(os.Stdout, "\t%d", nb)
			}
		}
		fmt.Fprintf(os.Stdout, "\n")
	}
	return
}

// Prints the Character with the most frequency
// for each site of the alignment
func printMaxCharStats(align align.Alignment, excludeGaps bool) {
	maxchars, occur := align.MaxCharStats(excludeGaps)

	fmt.Fprintf(os.Stdout, "site\tchar\tnb\n")
	for i, c := range maxchars {
		fmt.Fprintf(os.Stdout, "%d\t%c\t%d\n", i, c, occur[i])
	}
}

func init() {
	RootCmd.AddCommand(statsCmd)
}
