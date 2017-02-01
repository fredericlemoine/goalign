package align

import (
	"fmt"
	"strings"
	"testing"
)

func TestRandomAlignment(t *testing.T) {
	length := 3000
	nbseqs := 500
	a, err := RandomAlignment(AMINOACIDS, length, nbseqs)
	if err != nil {
		t.Error(err)
	}

	if a.Length() != length {
		t.Error(fmt.Sprintf("Length should be %d and is %d", length, a.Length()))
	}
	if a.NbSequences() != nbseqs {
		t.Error(fmt.Sprintf("Nb sequences should be %d and is %d", nbseqs, a.NbSequences()))
	}
}

func TestAppendIdentifier(t *testing.T) {
	a, err := RandomAlignment(AMINOACIDS, 300, 50)
	if err != nil {
		t.Error(err)

	}
	a.AppendSeqIdentifier("IDENT", false)

	a.IterateChar(func(name string, sequence []rune) {
		if !strings.HasPrefix(name, "IDENT") {
			t.Error("Sequence name does not start with expected id: IDENT")
		}
	})

	a.AppendSeqIdentifier("IDENT", true)
	a.IterateChar(func(name string, sequence []rune) {
		if !strings.HasSuffix(name, "IDENT") {
			t.Error("Sequence name does not end with expected id: IDENT")
		}
	})
}

func TestRemoveOneGaps(t *testing.T) {
	a, err := RandomAlignment(AMINOACIDS, 300, 300)
	if err != nil {
		t.Error(err)

	}

	/* We add 1 gap per site */
	pos := 0
	a.IterateChar(func(name string, sequence []rune) {
		sequence[pos] = GAP
		pos++
	})

	a.RemoveGaps(false)

	if a.Length() != 0 {
		t.Error("We should have removed all positions")
	}
	a.IterateChar(func(name string, sequence []rune) {
		if len(sequence) != 0 {
			t.Error(fmt.Sprintf("Sequence length after removing gaps should be 0 and is : %d", len(sequence)))
		}
	})
}

func TestRemoveAllGaps(t *testing.T) {
	a, err := RandomAlignment(AMINOACIDS, 300, 300)
	if err != nil {
		t.Error(err)

	}

	backupseq := make([]rune, 0, 300)
	seq0, found := a.GetSequenceChar(0)
	if !found {
		t.Error("Problem finding first sequence")
	}

	/* We add all gaps on 1 site */
	/* And one gap at all sites */
	pos1 := 20
	pos2 := 0
	a.IterateChar(func(name string, sequence []rune) {
		sequence[pos1] = GAP
		sequence[pos2] = GAP
		pos2++
	})
	backupseq = append(backupseq, seq0...)
	/* Remove position 20 */
	backupseq = append(backupseq[:20], backupseq[21:]...)

	a.RemoveGaps(true)

	if a.Length() != 299 {
		t.Error("We should have removed only one position")
	}

	a.IterateChar(func(name string, sequence []rune) {
		if len(sequence) != 299 {
			t.Error(fmt.Sprintf("Sequence length after removing gaps should be 299 and is : %d", len(sequence)))
		}
	})

	newseq, found2 := a.GetSequenceChar(0)
	if !found2 {
		t.Error("Problem finding first seqence")
	}

	for i, c := range newseq {
		if c != backupseq[i] {
			t.Error(fmt.Sprintf("Char at position %d should be %c and is %c", i, backupseq[i], c))
		}
	}
}

func TestClone(t *testing.T) {
	a, err := RandomAlignment(AMINOACIDS, 300, 300)
	if err != nil {
		t.Error(err)

	}

	/* We add 1 gap per site */
	pos := 0
	a.IterateChar(func(name string, sequence []rune) {
		sequence[pos] = GAP
		pos++
	})

	a2, err2 := a.Clone()
	if err2 != nil {
		t.Error(err2)
	}

	a.RemoveGaps(false)

	a2.IterateChar(func(name string, sequence []rune) {
		if len(sequence) != 300 {
			t.Error(fmt.Sprintf("Clone lenght should be 300 and is : %d", len(sequence)))
		}
	})
}

func TestClone2(t *testing.T) {
	a, err := RandomAlignment(AMINOACIDS, 300, 300)
	if err != nil {
		t.Error(err)

	}

	a2, err2 := a.Clone()
	if err2 != nil {
		t.Error(err2)
	}

	i := 0
	a2.IterateChar(func(name string, sequence []rune) {
		s2, ok := a.GetSequenceChar(i)
		n2, ok2 := a.GetSequenceName(i)

		if !ok || !ok2 {
			t.Error(fmt.Sprintf("Sequence not found in clone alignment: %s", name))
		}

		if len(sequence) != len(s2) {
			t.Error(fmt.Sprintf("Clone length is different from original length : %d != %d", len(sequence), len(s2)))
		}
		if name != n2 {
			t.Error(fmt.Sprintf("Clone and original sequences at position %d have different names : %s != %s", name, n2))
		}
		for j, c := range sequence {
			if c != s2[j] {
				t.Error(fmt.Sprintf("Clone sequence is different from original at position %d : %c != %c", j, c, s2[j]))
			}
		}
		i++
	})
}

func TestAvgAlleles(t *testing.T) {
	a, err := RandomAlignment(AMINOACIDS, 300, 300)
	if err != nil {
		t.Error(err)

	}

	a.IterateChar(func(name string, sequence []rune) {
		for j, _ := range sequence {
			sequence[j] = 'A'
		}
	})

	if a.AvgAllelesPerSite() != 1 {
		t.Error("There should be 1 allele per site in this alignment")
	}
}

func TestAvgAlleles2(t *testing.T) {
	a, err := RandomAlignment(AMINOACIDS, 300, 300)
	if err != nil {
		t.Error(err)

	}

	i := 0
	a.IterateChar(func(name string, sequence []rune) {
		for j, _ := range sequence {
			/* One only gap sequence */
			if i == 10 {
				sequence[j] = GAP
			} else if i <= 75 {
				sequence[j] = 'A'
			} else if i <= 150 {
				sequence[j] = 'C'
			} else if i <= 225 {
				sequence[j] = 'G'
			} else {
				sequence[j] = 'T'
			}
		}
		// Add a gap at a whole position
		sequence[50] = GAP
		i++
	})
	fmt.Println(a.AvgAllelesPerSite())
	if a.AvgAllelesPerSite() != 4 {
		t.Error("There should be 4 allele per site in this alignment")
	}
}