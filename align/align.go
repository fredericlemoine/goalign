package align

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"strings"
	"unicode"

	"github.com/armon/go-radix"
	"github.com/evolbioinfo/goalign/io"
)

type Alignment interface {
	SeqBag
	AddGaps(rate, lenprop float64)
	AvgAllelesPerSite() float64
	BuildBootstrap() Alignment
	CharStatsSite(site int) (map[rune]int, error)
	Clone() (Alignment, error)
	CodonAlign(ntseqs SeqBag) (codonAl *align, err error)
	// Remove identical patterns/sites and return number of occurence
	// of each pattern (order of patterns/sites may have changed)
	Compress() []int
	// concatenates the given alignment with this alignment
	Concat(Alignment) error
	// Compares all sequences to the first one and counts all differences per sequence
	//
	// - alldiffs: The set of all differences that have been seen at least once
	// - diffs   : The number of occurences of each difference, for each sequence
	//             Sequences are ordered as the original alignment. Differences are
	//             written as REFNEW, ex: diffs["AC"]=12 .
	CountDifferences() (alldiffs []string, diffs []map[string]int)
	// Compares all sequences to the first one and replace identical characters with .
	DiffWithFirst()
	Entropy(site int, removegaps bool) (float64, error) // Entropy of the given site
	// Positions of potential frameshifts
	// if startinggapsasincomplete is true, then considers gaps as the beginning
	// as incomplete sequence, then take the right phase
	Frameshifts(startingGapsAsIncomplete bool) []struct{ Start, End int }
	// Positions of potential stop in frame
	// if startinggapsasincomplete is true, then considers gaps as the beginning
	// as incomplete sequence, then take the right phase
	Stops(startingGapsAsIncomplete bool, geneticode int) (stops []int, err error)
	Length() int                  // Length of the alignment
	Mask(start, length int) error // Masks given positions
	MaxCharStats() ([]rune, []int)
	Mutate(rate float64)                                                                        // Adds uniform substitutions in the alignment (~sequencing errors)
	NbVariableSites() int                                                                       // Nb of variable sites
	Pssm(log bool, pseudocount float64, normalization int) (pssm map[rune][]float64, err error) // Normalization: PSSM_NORM_NONE, PSSM_NORM_UNIF, PSSM_NORM_DATA
	Rarefy(nb int, counts map[string]int) (Alignment, error)                                    // Take a new rarefied sample taking into accounts weights
	RandSubAlign(length int) (Alignment, error)                                                 // Extract a random subalignment with given length from this alignment
	Recombine(rate float64, lenprop float64)
	RemoveGapSeqs(cutoff float64)             // Removes sequences having >= cutoff gaps
	RemoveGapSites(cutoff float64, ends bool) // Removes sites having >= cutoff gaps
	// Replaces match characters (.) by their corresponding characters on the first sequence
	ReplaceMatchChars()
	Sample(nb int) (Alignment, error) // generate a sub sample of the sequences
	ShuffleSites(rate float64, roguerate float64, randroguefirst bool) []string
	SimulateRogue(prop float64, proplen float64) ([]string, []string) // add "rogue" sequences
	SiteConservation(position int) (int, error)                       // If the site is conserved:
	SubAlign(start, length int) (Alignment, error)                    // Extract a subalignment from this alignment
	Swap(rate float64)
	TrimSequences(trimsize int, fromStart bool) error
}

type align struct {
	seqbag
	length int // Length of alignment
}

type AlignChannel struct {
	Achan chan Alignment
	Err   error
}

func NewAlign(alphabet int) *align {
	switch alphabet {
	case AMINOACIDS, NUCLEOTIDS, UNKNOWN:
		// OK
	case BOTH:
		alphabet = NUCLEOTIDS
	default:
		io.ExitWithMessage(errors.New("Unexpected sequence alphabet type"))
	}
	return &align{
		seqbag{
			make(map[string]*seq),
			make([]*seq, 0, 100),
			alphabet},
		-1,
	}
}

func AlphabetFromString(alphabet string) int {
	switch strings.ToLower(alphabet) {
	case "dna", "rna", "nucleotide":
		return NUCLEOTIDS
	case "protein":
		return AMINOACIDS
	default:
		return UNKNOWN
	}
}

// Adds a sequence to this alignment
func (a *align) AddSequence(name string, sequence string, comment string) error {
	err := a.AddSequenceChar(name, []rune(sequence), comment)
	return err
}

func (a *align) AddSequenceChar(name string, sequence []rune, comment string) error {
	_, ok := a.seqmap[name]
	idx := 0
	tmpname := name
	/* If the sequence name already exists, we add a 4 digit index at the end and print a warning on stderr */
	for ok {
		idx++
		log.Print(fmt.Sprintf("Warning: sequence \"%s\" already exists in alignment, renamed in \"%s_%04d\"", tmpname, name, idx))
		tmpname = fmt.Sprintf("%s_%04d", name, idx)
		_, ok = a.seqmap[tmpname]
		/*return errors.New("Sequence " + name + " already exists in alignment")*/
	}

	if a.length != -1 && a.length != len(sequence) {
		return errors.New("Sequence " + tmpname + " does not have same length as other sequences")
	}
	a.length = len(sequence)
	seq := NewSequence(tmpname, sequence, comment)
	a.seqmap[tmpname] = seq
	a.seqs = append(a.seqs, seq)
	return nil
}

func (a *align) Clear() {
	a.seqbag.Clear()
	a.length = -1
}

func (a *align) Length() int {
	return a.length
}

// Shuffles vertically rate sites of the alignment
// randomly
// rate must be >=0 and <=1
// Then, take roguerate proportion of the taxa, and will shuffle rate sites among the
// remaining intact sites
// randroguefirst: If true, then with a given seed, rogues will always be the same with all alignments
// having sequences in the same order. It may not be the case if false, especially when alignemnts
// have different lengths.
// Output: List of tax names that are more shuffled than others (length=roguerate*nbsequences)
func (a *align) ShuffleSites(rate float64, roguerate float64, randroguefirst bool) []string {
	var sitepermutation, taxpermutation []int

	if rate < 0 || rate > 1 {
		io.ExitWithMessage(errors.New("Shuffle site rate must be >=0 and <=1"))
	}
	if roguerate < 0 || roguerate > 1 {
		io.ExitWithMessage(errors.New("Shuffle rogue rate must be >=0 and <=1"))
	}

	nb_sites_to_shuffle := int(rate * float64(a.Length()))
	nb_rogue_sites_to_shuffle := int(rate * (1.0 - rate) * (float64(a.Length())))
	nb_rogue_seq_to_shuffle := int(roguerate * float64(a.NbSequences()))
	if randroguefirst {
		taxpermutation = rand.Perm(a.NbSequences())
		sitepermutation = rand.Perm(a.Length())
	} else {
		sitepermutation = rand.Perm(a.Length())
		taxpermutation = rand.Perm(a.NbSequences())
	}

	rogues := make([]string, nb_rogue_seq_to_shuffle)

	if (nb_rogue_sites_to_shuffle + nb_sites_to_shuffle) > a.Length() {
		io.ExitWithMessage(errors.New(fmt.Sprintf("Too many sites to shuffle (%d+%d>%d)",
			nb_rogue_sites_to_shuffle, nb_sites_to_shuffle, a.Length())))
	}

	var temp rune
	for i := 0; i < nb_sites_to_shuffle; i++ {
		site := sitepermutation[i]
		var n int = a.NbSequences()
		for n > 1 {
			r := rand.Intn(n)
			n--
			temp = a.seqs[n].sequence[site]
			a.seqs[n].sequence[site] = a.seqs[r].sequence[site]
			a.seqs[r].sequence[site] = temp
		}
	}
	// We shuffle more sites for "rogue" taxa
	for i := 0; i < nb_rogue_sites_to_shuffle; i++ {
		site := sitepermutation[i+nb_sites_to_shuffle]
		for r := 0; r < nb_rogue_seq_to_shuffle; r++ {
			j := rand.Intn(r + 1)
			seq1 := a.seqs[taxpermutation[r]]
			seq2 := a.seqs[taxpermutation[j]]
			seq1.sequence[site], seq2.sequence[site] = seq2.sequence[site], seq1.sequence[site]
			rogues[r] = seq1.name
		}
	}
	return rogues
}

// Removes positions constituted of [cutoff*100%,100%] Gaps
// Exception fo a cutoff of 0: does not remove positions with 0% gaps
// Cutoff must be between 0 and 1, otherwise set to 0.
// 0 means that positions with > 0 gaps will be removed
// other cutoffs : ]0,1] mean that positions with >= cutoff gaps will be removed
//
// If ends is true: then only removes consecutive positions that match the cutoff
// from start or from end of alignment.
// Example with a cutoff of 0.3 and ends and with the given proportion of gaps:
// 0.4 0.5 0.1 0.5 0.6 0.1 0.8 will remove positions 0,1 and 6
func (a *align) RemoveGapSites(cutoff float64, ends bool) {
	var nbgaps int
	if cutoff < 0 || cutoff > 1 {
		cutoff = 0
	}

	toremove := make([]int, 0, 10)
	// To remove only gap positions at start and ends positions
	firstcontinuous := -1
	lastcontinuous := a.Length()

	for site := 0; site < a.Length(); site++ {
		nbgaps = 0
		for seq := 0; seq < a.NbSequences(); seq++ {
			if a.seqs[seq].sequence[site] == GAP {
				nbgaps++
			}
		}
		if (cutoff > 0.0 && float64(nbgaps) >= cutoff*float64(a.NbSequences())) || (cutoff == 0 && nbgaps > 0) {
			toremove = append(toremove, site)
			if site == firstcontinuous+1 {
				firstcontinuous++
			}
		}
	}
	/* Now we remove gap positions, starting at the end */
	sort.Ints(toremove)
	nbremoved := 0
	for i := (len(toremove) - 1); i >= 0; i-- {
		if toremove[i] == lastcontinuous-1 {
			lastcontinuous--
		}

		if !ends || lastcontinuous == toremove[i] || toremove[i] <= firstcontinuous {
			nbremoved++
			for seq := 0; seq < a.NbSequences(); seq++ {
				a.seqs[seq].sequence = append(a.seqs[seq].sequence[:toremove[i]], a.seqs[seq].sequence[toremove[i]+1:]...)
			}
		}
	}
	a.length -= nbremoved
}

// Removes sequences constituted of [cutoff*100%,100%] Gaps
// Exception fo a cutoff of 0: does not remove sequences with 0% gaps
// Cutoff must be between 0 and 1, otherwise set to 0.
// 0 means that sequences with > 0 gaps will be removed
// other cutoffs : ]0,1] mean that sequences with >= cutoff gaps will be removed
func (a *align) RemoveGapSeqs(cutoff float64) {
	var nbgaps int
	if cutoff < 0 || cutoff > 1 {
		cutoff = 0
	}

	toremove := make([]int, 0, 10)
	for seq := 0; seq < a.NbSequences(); seq++ {
		nbgaps = 0
		for site := 0; site < a.Length(); site++ {
			if a.seqs[seq].sequence[site] == GAP {
				nbgaps++
			}
		}
		if (cutoff > 0.0 && float64(nbgaps) >= cutoff*float64(a.Length())) || (cutoff == 0 && nbgaps > 0) {
			toremove = append(toremove, seq)
		}
	}
	/* Now we remove gap sequences, starting at the end */
	sort.Ints(toremove)
	for i := (len(toremove) - 1); i >= 0; i-- {
		a.seqs = append(a.seqs[:toremove[i]], a.seqs[toremove[i]+1:]...)
	}
}

// Swaps a rate of the sequences together
// takes rate/2 seqs and swap a part of them with the other
// rate/2 seqs at a random position
// if rate < 0 : does nothing
// if rate > 1 : does nothing
func (a *align) Swap(rate float64) {
	var nb_to_shuffle, nb_sites int
	var pos int
	var tmpchar rune
	var seq1, seq2 *seq

	if rate < 0 || rate > 1 {
		return
	}
	nb_sites = a.Length()
	nb_to_shuffle = (int)(rate * float64(a.NbSequences()))

	permutation := rand.Perm(a.NbSequences())

	for i := 0; i < int(nb_to_shuffle/2); i++ {
		// We take a random position in the sequences and swap both
		pos = rand.Intn(nb_sites)
		seq1 = a.seqs[permutation[i]]
		seq2 = a.seqs[permutation[i+(int)(nb_to_shuffle/2)]]
		for pos < nb_sites {
			tmpchar = seq1.sequence[pos]
			seq1.sequence[pos] = seq2.sequence[pos]
			seq2.sequence[pos] = tmpchar
			pos++
		}
	}
}

// Replaces match characters (.) by their corresponding characters on the first sequence
//
// If the correspongind character in the first sequence is also a ".", then leaves it unchanged.
func (a *align) ReplaceMatchChars() {
	if a.NbSequences() <= 1 {
		return
	}
	ref := a.seqs[0]
	for seq := 1; seq < a.NbSequences(); seq++ {
		for site := 0; site < a.Length(); site++ {
			if ref.sequence[site] != POINT && a.seqs[seq].sequence[site] == POINT {
				a.seqs[seq].sequence[site] = ref.sequence[site]
			}
		}
	}
}

// Translates the alignment, and update the length of
// the alignment
func (a *align) Translate(phase int, geneticcode int) (err error) {
	err = a.seqbag.Translate(phase, geneticcode)
	if len(a.seqs) > 0 {
		a.length = len(a.seqs[0].sequence)
	} else {
		a.length = -1
	}
	return
}

// Recombines a rate of the sequences to another sequences
// takes rate/2 seqs and copy/paste a portion of them to the other
// rate/2 seqs at a random position
// if rate < 0 : does nothing
// if rate > 1 : does nothing
// prop must be <= 0.5 because it will recombine x% of seqs based on other x% of seqs
func (a *align) Recombine(prop float64, lenprop float64) {
	var seq1, seq2 *seq

	if prop < 0 || prop > 0.5 {
		return
	}
	if lenprop < 0 || lenprop > 1 {
		return
	}

	nb := int(prop * float64(a.NbSequences()))
	lentorecomb := int(lenprop * float64(a.Length()))
	permutation := rand.Perm(a.NbSequences())

	// We take a random position in the sequences between min and max
	for i := 0; i < nb; i++ {
		pos := rand.Intn(a.Length() - lentorecomb + 1)
		seq1 = a.seqs[permutation[i]]
		seq2 = a.seqs[permutation[i+nb]]
		for j := pos; j < pos+lentorecomb; j++ {
			seq1.sequence[j] = seq2.sequence[j]
		}
	}
}

// Add prop*100% gaps to lenprop*100% of the sequences
// if prop < 0 || lenprop<0 : does nothing
// if prop > 1 || lenprop>1 : does nothing
func (a *align) AddGaps(lenprop float64, prop float64) {
	if prop < 0 || prop > 1 {
		return
	}
	if lenprop < 0 || lenprop > 1 {
		return
	}

	nb := int(prop * float64(a.NbSequences()))
	nbgaps := int(lenprop * float64(a.Length()))
	permseqs := rand.Perm(a.NbSequences())

	// We take a random position in the sequences between min and max
	for i := 0; i < nb; i++ {
		permsites := rand.Perm(a.Length())
		seq := a.seqs[permseqs[i]]
		for j := 0; j < nbgaps; j++ {
			seq.sequence[permsites[j]] = GAP
		}
	}
}

// Add substitutions uniformly to the alignment
// if rate < 0 : does nothing
// if rate > 1 : rate=1
// It does not apply to gaps or other special characters
func (a *align) Mutate(rate float64) {
	if rate <= 0 {
		return
	}
	if rate > 1 {
		rate = 1
	}
	r := 0.0
	newchar := 0
	leng := a.Length()
	nb := a.NbSequences()
	// We take a random position in the sequences between min and max
	for i := 0; i < nb; i++ {
		seq := a.seqs[i]
		for j := 0; j < leng; j++ {
			r = rand.Float64()
			// We mutate only if rand is <= rate && character is not a gap
			// or a special character.
			// It takes a random nucleotide or amino acid uniformly
			if r <= rate && seq.sequence[j] != GAP && seq.sequence[j] != POINT && seq.sequence[j] != OTHER {
				if a.Alphabet() == AMINOACIDS {
					newchar = rand.Intn(len(stdaminoacid))
					seq.sequence[j] = stdaminoacid[newchar]
				} else {
					newchar = rand.Intn(len(stdnucleotides))
					seq.sequence[j] = stdnucleotides[newchar]
				}
			}
		}
	}
}

// Simulate rogue taxa in the alignment:
// take the proportion prop of sequences as rogue taxa => R
// For each t in R
//   * We shuffle the alignment sites of t
// Output: List of rogue sequence names, and List of intact sequence names
func (a *align) SimulateRogue(prop float64, proplen float64) ([]string, []string) {
	var seq *seq

	if prop < 0 || prop > 1.0 {
		return nil, nil
	}

	if proplen < 0 || proplen > 1.0 {
		return nil, nil
	}

	if proplen == 0 {
		prop = 0.0
	}

	nb := int(prop * float64(a.NbSequences()))
	permutation := rand.Perm(a.NbSequences())
	seqlist := make([]string, nb)
	intactlist := make([]string, a.NbSequences()-nb)
	len := int(proplen * float64(a.Length()))
	// For each chosen rogue sequence
	for r := 0; r < nb; r++ {
		seq = a.seqs[permutation[r]]
		seqlist[r] = seq.name
		sitesToShuffle := rand.Perm(a.Length())[0:len]
		// we Shuffle some sequence sites
		for i, _ := range sitesToShuffle {
			j := rand.Intn(i + 1)
			seq.sequence[sitesToShuffle[i]], seq.sequence[sitesToShuffle[j]] = seq.sequence[sitesToShuffle[j]], seq.sequence[sitesToShuffle[i]]
		}
	}
	for nr := nb; nr < a.NbSequences(); nr++ {
		seq = a.seqs[permutation[nr]]
		intactlist[nr-nb] = seq.name
	}
	return seqlist, intactlist
}

// Trims alignment sequences.
// If fromStart, then trims from the start, else trims from the end
// If trimsize >= sequence or trimsize < 0 lengths, then throw an error
func (a *align) TrimSequences(trimsize int, fromStart bool) error {
	if trimsize < 0 {
		return errors.New("Trim size must not be < 0")
	}
	if trimsize >= a.Length() {
		return errors.New("Trim size must be < alignment length (" + fmt.Sprintf("%d", a.Length()) + ")")
	}
	for _, seq := range a.seqs {
		if fromStart {
			seq.sequence = seq.sequence[trimsize:len(seq.sequence)]
		} else {
			seq.sequence = seq.sequence[0 : len(seq.sequence)-trimsize]
		}
	}
	a.length = a.length - trimsize
	return nil
}

// Samples randomly a subset of the sequences
// And returns this new alignment
// If nb < 1 or nb > nbsequences returns nil and an error
func (a *align) Sample(nb int) (Alignment, error) {
	if a.NbSequences() < nb {
		return nil, errors.New("Number of sequences to sample is greater than alignment size")
	}
	if nb < 1 {
		return nil, errors.New("Cannot sample less than 1 sequence")
	}
	sample := NewAlign(a.alphabet)
	permutation := rand.Perm(a.NbSequences())
	for i := 0; i < nb; i++ {
		seq := a.seqs[permutation[i]]
		sample.AddSequenceChar(seq.name, seq.SequenceChar(), seq.Comment())
	}
	return sample, nil
}

/*
Each sequence in the alignment has an associated number of occurence. The sum s of the counts
represents the number of sequences in the underlying initial dataset.

The goal is to downsample (rarefy) the initial dataset, by sampling n sequences
from s (n<s), and taking the alignment corresponding to this new sample, i.e by
taking only unique (different) sequences from it.

Parameters are:
* nb: the number of sequences to sample from the underlying full dataset (different
from the number of sequences in the output alignment)
* counts: counts associated to each sequence (if the count of a sequence is missing, it
is considered as 0, if the count of an unkown sequence is present, it will return an error).
 Sum of counts of all sequences must be > n.
*/
func (a *align) Rarefy(nb int, counts map[string]int) (Alignment, error) {
	// Sequences that will be selected
	selected := make(map[string]bool)
	total := 0
	// We copy the count map to modify it
	tmpcounts := make(map[string]int)
	tmpcountskeys := make([]string, len(counts))
	i := 0
	for k, v := range counts {
		tmpcountskeys[i] = k
		if v <= 0 {
			return nil, errors.New("Sequence counts must be positive")
		}
		if _, ok := a.GetSequenceChar(k); !ok {
			return nil, errors.New(fmt.Sprintf("Sequence %s does not exist in the alignment", k))
		}
		tmpcounts[k] = v
		total += v
		i++
	}

	sort.Strings(tmpcountskeys)

	if nb >= total {
		return nil, errors.New(fmt.Sprintf("Number of sequences to sample %d is >= sum of the counts %d", nb, total))
	}

	// We sample a new sequence nb times
	for i := 0; i < nb; i++ {
		proba := 0.0
		// random num
		unif := rand.Float64()
		for idk, k := range tmpcountskeys {
			v, ok := tmpcounts[k]
			if !ok {
				return nil, errors.New(fmt.Sprintf("No sequence named %s is present in the tmp count map", k))
			}
			proba += float64(v) / float64(total)
			if unif < proba {
				selected[k] = true
				if v-1 == 0 {
					delete(tmpcounts, k)
					tmpcountskeys = append(tmpcountskeys[:idk], tmpcountskeys[idk+1:]...)
				} else {
					tmpcounts[k] = v - 1
				}
				break
			}
		}
		total--
	}

	sample := NewAlign(a.alphabet)
	a.IterateAll(func(name string, sequence []rune, comment string) {
		if _, ok := selected[name]; ok {
			sample.AddSequenceChar(name, sequence, comment)
		}
	})

	return sample, nil
}

func (a *align) BuildBootstrap() Alignment {
	n := a.Length()
	boot := NewAlign(a.alphabet)
	indices := make([]int, n)
	var buf []rune

	for i := 0; i < n; i++ {
		indices[i] = rand.Intn(n)
	}

	for _, seq := range a.seqs {
		buf = make([]rune, n)
		for i, indice := range indices {
			buf[i] = seq.sequence[indice]
		}
		boot.AddSequenceChar(seq.name, buf, seq.Comment())
	}
	return boot
}

// Returns the distribution of characters at a given site
// if the site index is outside alignment, returns an error
func (a *align) CharStatsSite(site int) (outmap map[rune]int, err error) {
	outmap = make(map[rune]int)

	if site < 0 || site >= a.Length() {
		err = errors.New("Cannot compute site char statistics: Site index is outside alignment")
	} else {

		for _, s := range a.seqs {
			outmap[unicode.ToUpper(s.sequence[site])]++
		}
	}
	return outmap, err
}

func (a *align) Mask(start, length int) (err error) {
	if start < 0 {
		err = errors.New("Mask: Start position cannot be < 0")
		return
	}
	if start > a.Length() {
		err = errors.New("Mask: Start position cannot be > align length")
		return
	}

	rep := '.'
	if a.Alphabet() == AMINOACIDS {
		rep = ALL_AMINO
	} else if a.Alphabet() == NUCLEOTIDS {
		rep = ALL_NUCLE
	} else {
		err = errors.New("Mask: Cannot mask alignment, wrong alphabet")
	}

	for _, seq := range a.seqs {
		for i := start; i < (start+length) && i < a.Length(); i++ {
			seq.sequence[i] = rep
		}
	}
	return
}

// Returns the Character with the most occurences
// for each site of the alignment
func (a *align) MaxCharStats() (out []rune, occur []int) {
	out = make([]rune, a.Length())
	occur = make([]int, a.Length())
	for site := 0; site < a.Length(); site++ {
		mapstats := make(map[rune]int)
		max := 0
		for _, seq := range a.seqs {
			mapstats[unicode.ToUpper(seq.sequence[site])]++
		}
		for k, v := range mapstats {
			if v > max {
				out[site] = k
				occur[site] = v
				max = v
			}
		}
	}

	return out, occur
}

func RandomAlignment(alphabet, length, nbseq int) (Alignment, error) {
	al := NewAlign(alphabet)
	for i := 0; i < nbseq; i++ {
		name := fmt.Sprintf("Seq%04d", i)
		if seq, err := RandomSequence(alphabet, length); err != nil {
			return nil, err
		} else {
			al.AddSequenceChar(name, seq, "")
		}
	}
	return al, nil
}

func (a *align) Clone() (c Alignment, err error) {
	c = NewAlign(a.Alphabet())
	a.IterateAll(func(name string, sequence []rune, comment string) {
		newseq := make([]rune, 0, len(sequence))
		newseq = append(newseq, sequence...)
		err = c.AddSequenceChar(name, newseq, comment)
		if err != nil {
			return
		}
	})
	return
}

func (a *align) AvgAllelesPerSite() float64 {
	nballeles := 0
	nbsites := 0
	for site := 0; site < a.Length(); site++ {
		alleles := make(map[rune]bool)
		onlygap := true
		for seq := 0; seq < a.NbSequences(); seq++ {
			s := a.seqs[seq].sequence[site]
			if s != GAP && s != POINT && s != OTHER {
				alleles[s] = true
				onlygap = false
			}
		}
		if !onlygap {
			nbsites++
		}
		nballeles += len(alleles)
	}
	return float64(nballeles) / float64(nbsites)
}

// Entropy of the given site. If the site number is < 0 or > length -> returns an error
// if removegaps is true, do not take into account gap characters
func (a *align) Entropy(site int, removegaps bool) (float64, error) {
	if site < 0 || site > a.Length() {
		return 1.0, errors.New("Site position is outside alignment")
	}

	// Number of occurences of each different aa/nt
	occur := make(map[rune]int)
	total := 0
	entropy := 0.0
	for seq := 0; seq < a.NbSequences(); seq++ {
		s := a.seqs[seq].sequence[site]
		if s != OTHER && s != POINT && (!removegaps || s != GAP) {
			nb, ok := occur[s]
			if !ok {
				occur[s] = 1
			} else {
				occur[s] = nb + 1
			}
			total++
		}
	}

	for _, v := range occur {
		proba := float64(v) / float64(total)
		entropy -= proba * math.Log(proba)
	}

	if total == 0 {
		return math.NaN(), nil
	}
	return entropy, nil
}

// First sequence of the alignment is considered as the reference orf (in phase)
// It return for each sequence the coordinates of the longest dephased part
func (a *align) Frameshifts(startingGapsAsIncomplete bool) (fs []struct{ Start, End int }) {
	ref := a.seqs[0]
	fs = make([]struct{ Start, End int }, a.NbSequences())
	for s := 1; s < a.NbSequences(); s++ {
		fs[s].Start = 0
		fs[s].End = 0
		seq := a.seqs[s]
		phase := 0 // in frame
		start := 0 // Start of dephasing
		pos := 0
		started := false
		for i := 0; i < a.Length(); i++ {
			// Insertion in seq
			if ref.sequence[i] == '-' {
				phase++
				phase = (phase % 3)
			}
			// Deletion in seq
			if seq.sequence[i] == '-' {
				phase--
				if phase < 0 {
					phase = 2
				}
			} else if !started && startingGapsAsIncomplete && phase != 0 {
				phase--
				if phase < 0 {
					phase = 2
				}
			} else {
				started = true
				pos++
			}

			// If we go back in the good phase (or we are at the end of the sequence:
			// we add a frameshift if it is longer than the previous one
			if (phase == 0 || i == a.Length()-1) && pos-start > 1 && pos-start > fs[s].End-fs[s].Start {
				fs[s].Start = start
				fs[s].End = pos
			}

			if phase == 0 {
				start = pos
			}
		}
	}
	return
}

// Position of the first encountered STOP in frame
func (a *align) Stops(startingGapsAsIncomplete bool, geneticcode int) (stops []int, err error) {
	var code map[string]rune

	if code, err = geneticCode(geneticcode); err != nil {
		return
	}

	stops = make([]int, a.NbSequences())
	codon := make([]rune, 3)
	ref := a.seqs[0]
	phase := 0
	started := false
	for s := 1; s < a.NbSequences(); s++ {
		seq := a.seqs[s]
		stops[s] = -1
		pos := 0      // position on sequence (without -)
		codonpos := 0 // nb nt in current codon
		for i := 0; i < a.Length()-2; i++ {
			if ref.sequence[i] == '-' {
				phase++
				phase = (phase % 3)
			}
			// Deletion in seq
			if seq.sequence[i] == '-' {
				phase--
				if phase < 0 {
					phase = 2
				}
			} else if !started && startingGapsAsIncomplete && phase != 0 {
				phase--
				if phase < 0 {
					phase = 2
				}
			} else {
				started = true
			}

			// Deletion in seq
			if seq.sequence[i] != '-' && (!startingGapsAsIncomplete || started) {
				codon[codonpos] = seq.sequence[i]
				codonpos++
				pos++
			}

			if codonpos == 3 {
				codonstr := strings.Replace(strings.ToUpper(string(codon)), "U", "T", -1)
				aa, found := code[codonstr]
				if !found {
					aa = 'X'
				}
				if aa == '*' {
					stops[s] = pos
					break
				}
				codonpos = 0
			}
		}
	}
	return
}

/* Computes a position-specific scoring matrix (PSSM)matrix
(see https://en.wikipedia.org/wiki/Position_weight_matrix)
This matrix may be in log2 scale or not (log argument)
A pseudo count may be added to values (to avoid log2(0))) with pseudocount argument
values may be normalized: normalization arg:
   PSSM_NORM_NONE = 0 => No normalization
   PSSM_NORM_FREQ = 1 => Normalization by frequency in the site
   PSSM_NORM_DATA = 2 => Normalization by frequency in the site and divided by aa/nt frequency in data
   PSSM_NORM_UNIF = 3 => Normalization by frequency in the site and divided by uniform frequency (1/4 or 1/20)
   PSSM_NORM_LOGO = 4 => Normalization like "Logo"
*/
func (a *align) Pssm(log bool, pseudocount float64, normalization int) (pssm map[rune][]float64, err error) {
	// Number of occurences of each different aa/nt
	pssm = make(map[rune][]float64)
	var alphabet []rune
	var normfactors map[rune]float64
	/* Entropy at each position */
	var entropy []float64
	alphabet = a.AlphabetCharacters()
	for _, c := range alphabet {
		if _, ok := pssm[c]; !ok {
			pssm[c] = make([]float64, a.Length())
		}
	}

	/* We compute normalization factors (takes into account pseudo counts) */
	normfactors = make(map[rune]float64)
	switch normalization {
	case PSSM_NORM_NONE:
		for _, c := range alphabet {
			normfactors[c] = 1.0
		}
	case PSSM_NORM_UNIF:
		for _, c := range alphabet {
			normfactors[c] = 1.0 / (float64(a.NbSequences()) + (float64(len(pssm)) * pseudocount)) / (1.0 / float64(len(alphabet)))
		}
	case PSSM_NORM_FREQ:
		for _, c := range alphabet {
			normfactors[c] = 1.0 / (float64(a.NbSequences()) + (float64(len(pssm)) * pseudocount))
		}
	case PSSM_NORM_LOGO:
		for _, c := range alphabet {
			normfactors[c] = 1.0 / float64(a.NbSequences())
		}
	case PSSM_NORM_DATA:
		stats := a.CharStats()
		total := 0.0
		for _, c := range alphabet {
			if s, ok := stats[c]; !ok {
				err = errors.New(fmt.Sprintf("No charchacter %c in alignment statistics", c))
				return
			} else {
				total += float64(s)
			}
		}
		for _, c := range alphabet {
			s, _ := stats[c]
			normfactors[c] = 1.0 / (float64(a.NbSequences()) + (float64(len(pssm)) * pseudocount)) / (float64(s) / total)
		}
	default:
		err = errors.New("Unknown normalization option")
		return
	}

	/* We count nt/aa occurences at each site */
	for site := 0; site < a.Length(); site++ {
		for seq := 0; seq < a.NbSequences(); seq++ {
			s := a.seqs[seq].sequence[site]
			if _, ok := normfactors[s]; ok {
				if _, ok := pssm[s]; ok {
					pssm[s][site] += 1.0
				}
			}
		}
	}

	/* We add pseudo counts */
	if pseudocount > 0 {
		for _, v := range pssm {
			for i, _ := range v {
				v[i] += pseudocount
			}
		}
	}

	/* Initialize entropy if NORM_LOGO*/
	entropy = make([]float64, a.Length())
	/* Applying normalization factors */
	for k, v := range pssm {
		for i, _ := range v {
			v[i] = v[i] * normfactors[k]
			if normalization == PSSM_NORM_LOGO {
				entropy[i] += -v[i] * math.Log(v[i]) / math.Log(2)
			}
		}
	}

	/* We compute the logo */
	if normalization == PSSM_NORM_LOGO {
		for _, v := range pssm {
			for i, _ := range v {
				v[i] = v[i] * (math.Log(float64(len(alphabet)))/math.Log(2) - entropy[i])
			}
		}
	} else {
		/* Applying log2 transform */
		if log {
			for _, v := range pssm {
				for i, _ := range v {
					v[i] = math.Log(v[i]) / math.Log(2)
				}
			}
		}
	}

	return
}

// Extract a subalignment from this alignment
func (a *align) SubAlign(start, length int) (Alignment, error) {
	if start < 0 || start > a.Length() {
		return nil, errors.New("Start is outside the alignment")
	}
	if start+length < 0 || start+length > a.Length() {
		return nil, errors.New("Start+Length is outside the alignment")
	}
	subalign := NewAlign(a.alphabet)
	for i := 0; i < a.NbSequences(); i++ {
		seq := a.seqs[i]
		subalign.AddSequenceChar(seq.name, seq.SequenceChar()[start:start+length], seq.Comment())
	}
	return subalign, nil
}

// Extract a subalignment with given length and a random start position from this alignment
func (a *align) RandSubAlign(length int) (Alignment, error) {
	if length > a.Length() {
		return nil, errors.New("sub alignment is larger than original alignment ")
	}
	if length <= 0 {
		return nil, errors.New("sub alignment cannot have 0 or negative length")
	}

	subalign := NewAlign(a.alphabet)
	start := rand.Intn(a.Length() - length + 1)
	for i := 0; i < a.NbSequences(); i++ {
		seq := a.seqs[i]
		subalign.AddSequenceChar(seq.name, seq.SequenceChar()[start:start+length], seq.Comment())
	}
	return subalign, nil
}

/*
Remove identical patterns/sites and return number of occurence
 of each pattern (order of patterns/sites may have changed)
*/
func (a *align) Compress() (weights []int) {
	var count interface{}
	var ok bool
	r := radix.New()
	npat := 0
	// We add new patterns if not already insterted in the radix tree
	for site := 0; site < a.Length(); site++ {
		pattern := make([]rune, a.NbSequences())
		for seq := 0; seq < a.NbSequences(); seq++ {
			pattern[seq] = a.seqs[seq].sequence[site]
		}
		patstring := string(pattern)
		if count, ok = r.Get(patstring); !ok {
			npat++
			count = &struct{ count int }{0}
		}
		count.(*struct{ count int }).count++
		r.Insert(patstring, count)
	}
	// Init weights
	weights = make([]int, npat)
	// We add the patterns
	npat = 0
	r.Walk(func(pattern string, count interface{}) bool {
		weights[npat] = count.(*struct{ count int }).count
		for seq, c := range pattern {
			a.seqs[seq].sequence[npat] = c
		}
		npat++
		return false
	})
	// We remove what remains of the sequences after al patterns
	for seq := 0; seq < a.NbSequences(); seq++ {
		a.seqs[seq].sequence = a.seqs[seq].sequence[:npat]
	}
	a.length = npat
	return
}

/*
Concatenates both alignments. It appends the given alignment to this alignment.
If a sequence is present in this alignment and not in c, then it adds a full gap sequence.
If a sequence is present in c alignment and not in this, then it appends the new sequence
to a full gap sequence.
Returns an error if the sequences do not have the same alphabet.
*/
func (a *align) Concat(c Alignment) (err error) {
	if a.Alphabet() != c.Alphabet() {
		return errors.New("Alignments do not have the same alphabet")
	}
	a.IterateAll(func(name string, sequence []rune, comment string) {
		_, ok := c.GetSequenceChar(name)
		if !ok {
			// This sequence is present in a but not in c
			// So we append full gap sequence to a
			a.appendToSequence(name, []rune(strings.Repeat(string(GAP), c.Length())))
		}
	})
	if err != nil {
		return err
	}
	c.IterateAll(func(name string, sequence []rune, comment string) {
		_, ok := a.GetSequenceChar(name)
		if !ok {
			// This sequence is present in c but not in a
			// So we add it to a, with gaps only
			a.AddSequence(name, strings.Repeat(string(GAP), a.Length()), comment)
		}
		// Then we append the c sequence to a
		err = a.appendToSequence(name, sequence)
	})
	if err != nil {
		return err
	}

	leng := -1
	a.IterateChar(func(name string, sequence []rune) {
		if leng == -1 {
			leng = len(sequence)
		} else {
			if leng != len(sequence) {
				err = errors.New("Sequences of the new alignment do not have the same length...")
			}
		}
	})
	a.length = leng

	return err
}

// Compares all sequences to the first one and replaces identical characters with .
func (a *align) DiffWithFirst() {
	var seqs []Sequence
	var first, other []rune
	var i, l int
	if a.NbSequences() < 2 {
		return
	}
	seqs = a.Sequences()
	first = seqs[0].SequenceChar()
	for i = 1; i < a.NbSequences(); i++ {
		other = seqs[i].SequenceChar()
		for l = 0; l < len(first); l++ {
			if first[l] == other[l] {
				other[l] = '.'
			}
		}
	}
}

// Compares all sequences to the first one and counts all differences per sequence
//
// - alldiffs: The set of all differences that have been seen at least once
// - diffs   : The number of occurences of each difference, for each sequence
//             Sequences are ordered as the original alignment. Differences are
//             written as REFNEW, ex: diffs["AC"]=12.
func (a *align) CountDifferences() (alldiffs []string, diffs []map[string]int) {
	var alldiffsmap map[string]bool
	var diffmap map[string]int
	var first, other []rune
	var key string
	var ok bool
	var i, l, count int
	var seqs []Sequence

	alldiffs = make([]string, 0)
	diffs = make([]map[string]int, a.NbSequences()-1)
	if a.NbSequences() < 2 {
		return
	}
	seqs = a.Sequences()
	alldiffsmap = make(map[string]bool, 0)
	first = seqs[0].SequenceChar()
	for i = 1; i < a.NbSequences(); i++ {
		diffmap = make(map[string]int)
		other = seqs[i].SequenceChar()
		for l = 0; l < len(first); l++ {
			if first[l] != other[l] {
				key = fmt.Sprintf("%c%c", first[l], other[l])
				count = diffmap[key]
				diffmap[key] = count + 1
				if _, ok = alldiffsmap[key]; !ok {
					alldiffs = append(alldiffs, key)
					alldiffsmap[key] = true
				}
			}
		}
		diffs[i-1] = diffmap
	}
	return
}

/*
 Returns the number of variable sites in the alignment.
It does not take into account gaps and other charactes like "."
*/
func (a *align) NbVariableSites() int {
	nbinfo := 0
	for site := 0; site < a.Length(); site++ {
		charmap := make(map[rune]bool)
		variable := false
		for _, seq := range a.seqs {
			if seq.sequence[site] != GAP && seq.sequence[site] != POINT && seq.sequence[site] != OTHER {
				charmap[seq.sequence[site]] = true
			}
			if len(charmap) > 1 {
				variable = true
				break
			}
		}
		if variable {
			nbinfo++
		}
	}
	return nbinfo
}

// Aligns given nt sequences (ntseqs) using a corresponding aa alignment (a).
//
// If a is not amino acid, then returns an error.
// If ntseqs is not nucleotides then returns an error.
//
// Warning: It does not check that the amino acid sequence is a good
// translation of the nucleotide sequence, but just adds gaps to the
// nucleotide sequence where needed.
//
// Once gaps are added, if the nucleotide alignment length does not match
// the protein alignment length * 3, returns an error.
func (a *align) CodonAlign(ntseqs SeqBag) (rtAl *align, err error) {
	var buffer bytes.Buffer

	if a.Alphabet() != AMINOACIDS {
		return nil, errors.New("Wrong alphabet, cannot reverse translate nucleotides")
	}

	if ntseqs.Alphabet() != NUCLEOTIDS {
		return nil, errors.New("Wrong nucleotidic alignment alphabet, cannot reverse translate")
	}

	rtAl = NewAlign(ntseqs.Alphabet())
	// outputting aligned codons
	a.IterateAll(func(name string, sequence []rune, comment string) {
		buffer.Reset()
		ntseq, ok := ntseqs.GetSequenceChar(name)
		if !ok {
			err = fmt.Errorf("Sequence %s is not present in the nucleotidic sequence, cannot reverse translate", name)
			return // return from this iteration of iterator
		}

		ntseqindex := 0
		for i := 0; i < len(sequence); i++ {
			if sequence[i] == '-' {
				buffer.WriteString("---")
			} else {
				if ntseqindex+3 > len(ntseq) {
					err = fmt.Errorf("Nucleotidic sequence %s is shorter than its aa counterpart", name)
					return // return from this iteration of iterator
				}
				buffer.WriteString(string(ntseq[ntseqindex : ntseqindex+3]))
				ntseqindex += 3
			}
		}
		if ntseqindex < len(ntseq) {
			// At most 2 remaining nucleotides that could not be part of the last codon
			if len(ntseq)-ntseqindex <= 2 {
				log.Print(fmt.Sprintf("%s: Dropping %d additional nucleotides", name, len(ntseq)-ntseqindex))
			} else {
				// A problem with the sequences
				err = fmt.Errorf("Nucleotidic sequence %s is longer than its aa counterpart (%d = more than 2 nucleotides remaining)", name, len(ntseq)-ntseqindex)
				return
			}
		}
		rtAl.AddSequence(name, buffer.String(), comment)
	})
	return
}

// Compute conservation status of a given site of the alignment
//
// If position is outside the alignment, it returns an error
//
// Possible values are:
//
// - align.POSITION_IDENTICAL
// - align.POSITION_CONSERVED
// - align.POSITION_SEMI_CONSERVED
// - align.POSITION_NOT_CONSERVED
func (a *align) SiteConservation(position int) (conservation int, err error) {
	conservation = POSITION_NOT_CONSERVED

	if position < 0 || position >= a.Length() {
		err = errors.New("Site conservation: Position is not in sequence length range")
		return
	}

	tmpstronggroups := make([]int, len(strongGroups))
	tmpweakgroups := make([]int, len(weakGroups))
	same := true
	prevchar := ';'
	a.IterateChar(func(name string, sequence []rune) {
		if a.Alphabet() == AMINOACIDS {
			for i, g := range strongGroups {
				for _, aa := range g {
					if aa == unicode.ToUpper(sequence[position]) {
						tmpstronggroups[i]++
					}
				}
			}
			for i, g := range weakGroups {
				for _, aa := range g {
					if aa == unicode.ToUpper(sequence[position]) {
						tmpweakgroups[i]++
					}
				}
			}
		}
		if (prevchar != ';' && sequence[position] != prevchar) || sequence[position] == GAP {
			same = false
		}
		prevchar = sequence[position]
	})

	if same {
		conservation = POSITION_IDENTICAL
	} else {
		for _, nb := range tmpstronggroups {
			if nb == a.NbSequences() {
				conservation = POSITION_CONSERVED
			}
		}
		if conservation != POSITION_CONSERVED {
			for _, nb := range tmpweakgroups {
				if nb == a.NbSequences() {
					conservation = POSITION_SEMI_CONSERVED
				}
			}
		}
	}

	return
}
