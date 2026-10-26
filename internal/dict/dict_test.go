package dict

import "testing"

func TestLookupFindsHeadwordsAndForms(t *testing.T) {
	dictionary, err := Open("../../resources/dictionary.db")
	if err != nil {
		t.Fatal(err)
	}
	if dictionary == nil {
		t.Fatal("the dictionary file is missing")
	}
	defer dictionary.Close()

	for _, test := range []struct{ in, want string }{
		{"sundials", "sundial"},
		{"Palimpsest", "palimpsest"},
		{"inkwell", "inkwell"},
	} {
		entry, ok, err := dictionary.Lookup(test.in)
		if err != nil {
			t.Fatalf("%s: %v", test.in, err)
		}
		if !ok {
			t.Fatalf("%s: not found", test.in)
		}
		if entry.Term != test.want {
			t.Errorf("%s: got %q, want %q", test.in, entry.Term, test.want)
		}
		if entry.ZH == "" {
			t.Errorf("%s: no Chinese meaning", test.in)
		}
	}

	if _, ok, err := dictionary.Lookup("zzzzzz"); err != nil || ok {
		t.Errorf("zzzzzz: got ok=%v err=%v", ok, err)
	}
}
