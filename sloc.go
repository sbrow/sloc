package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"runtime/pprof"
	"sort"
	"strings"
	"text/tabwriter"
)

const VERSION = `0.1.1`

var languages = []Language{
	{"Thrift", mExt(".thrift"), cComments},

	{"C", mExt(".c", ".h"), cComments},
	{"C++", mExt(".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx"), cComments},
	{"Go (test)", mExt("_test.go"), cComments},
	{"Go", mExt(".go"), cComments},
	{"Scala", mExt(".scala"), cComments},
	{"Java", mExt(".java"), cComments},

	{"YACC", mExt(".y"), cComments},
	{"Lex", mExt(".l"), cComments},

	{"SQL", mExt(".sql"), sqlComments},

	{"Haskell", mExt(".hs", ".lhs"), hsComments},

	{"Perl", mExt(".pl", ".pm"), shComments},
	{"PHP", mExt(".php"), cComments},

	{"Shell", mExt(".sh"), shComments},
	{"Bash", mExt(".bash"), shComments},
	{"Zsh", mExt(".zsh"), shComments},

	{"Ruby", mExt(".rb"), shComments},
	{"Python", mExt(".py"), pyComments},
	{"Assembly", mExt(".asm", ".s"), semiComments},
	{"Lisp", mExt(".lsp", ".lisp"), semiComments},
	{"Scheme", mExt(".scm", ".scheme"), semiComments},
	{"Lua", mExt(".lua"), luaComments},

	{"Make", mName("makefile", "Makefile", "MAKEFILE"), shComments},
	{"CMake", mName("CMakeLists.txt"), shComments},
	{"Jam", mName("Jamfile", "Jamrules"), shComments},

	{"Markdown", mExt(".md"), noComments},

	{"HAML", mExt(".haml"), noComments},
	{"SASS", mExt(".sass"), cssComments},
	{"SCSS", mExt(".scss"), cssComments},

	{"HTML", mExt(".htm", ".html", ".xhtml"), xmlComments},
	{"XML", mExt(".xml"), xmlComments},
	{"CSS", mExt(".css"), cssComments},
	{"JavaScript", mExt(".js"), cComments},
	{"ExtendScript", mExt(".jsx"), cComments},
	{"VBScript", mExt(".vbs"), vbComments},

	{"JSON", mExt(".json"), noComments},
	{"YAML", mExt(".yml"), pyComments},
}

type Commenter struct {
	LineComment  string
	StartComment string
	EndComment   string
	Nesting      bool
}

var (
	noComments   = Commenter{"\000", "\000", "\000", false}
	xmlComments  = Commenter{"\000", `<!--`, `-->`, false}
	cComments    = Commenter{`//`, `/*`, `*/`, false}
	cssComments  = Commenter{"\000", `/*`, `*/`, false}
	shComments   = Commenter{`#`, "\000", "\000", false}
	semiComments = Commenter{`;`, "\000", "\000", false}
	hsComments   = Commenter{`--`, `{-`, `-}`, true}
	sqlComments  = Commenter{`--`, "\000", "\000", false}
	pyComments   = Commenter{`#`, `"""`, `"""`, false}
	vbComments   = Commenter{"'", "\000", "\000", false}
	luaComments  = Commenter{"--", "--[[", "--]]", false}
)

type Language struct {
	Namer
	Matcher
	Commenter
}

// TODO work properly with unicode
func (l Language) Update(c []byte, s *Stats) {
	s.FileCount++

	inComment := 0 // this is an int for nesting
	inLComment := false
	blank := true
	lc := []byte(l.LineComment)
	sc := []byte(l.StartComment)
	ec := []byte(l.EndComment)
	lp, sp, ep := 0, 0, 0

	for _, b := range c {
		if inComment == 0 && b == lc[lp] {
			lp++
			if lp == len(lc) {
				inLComment = true
				lp = 0
			}
		} else {
			lp = 0
		}
		if !inLComment && b == sc[sp] {
			sp++
			if sp == len(sc) {
				inComment++
				if inComment > 1 && !l.Nesting {
					inComment = 1
				}
				sp = 0
			}
		} else {
			sp = 0
		}
		if !inLComment && inComment > 0 && b == ec[ep] {
			ep++
			if ep == len(ec) {
				if inComment > 0 {
					inComment--
				}
				ep = 0
			}
		} else {
			ep = 0
		}

		if b != byte(' ') && b != byte('\t') && b != byte('\n') && b != byte('\r') {
			blank = false
		}

		// BUG(srl): lines with comment don't count towards code
		// Note that lines with both code and comment count towards
		// each, but are not counted twice in the total.
		if b == byte('\n') {
			s.TotalLines++
			if inComment > 0 || inLComment {
				inLComment = false
				s.CommentLines++
			} else if blank {
				s.BlankLines++
			} else {
				s.CodeLines++
			}
			blank = true
			continue
		}
	}
}

type Namer string

func (l Namer) Name() string { return string(l) }

type Matcher func(string) bool

func (m Matcher) Match(fname string) bool { return m(fname) }

func mExt(exts ...string) Matcher {
	return func(fname string) bool {
		for _, ext := range exts {
			reg := regexp.MustCompile(strings.Replace(ext, ".", "\\.", -1) + "$")
			if reg.MatchString(fname) || ext == path.Ext(fname) {
				return true
			}
		}
		return false
	}
}

func mName(names ...string) Matcher {
	return func(fname string) bool {
		for _, name := range names {
			if name == path.Base(fname) {
				return true
			}
		}
		return false
	}
}

type Stats struct {
	FileCount    int
	TotalLines   int
	CodeLines    int
	BlankLines   int
	CommentLines int
}

var info = map[string]*Stats{}

func handleFile(fname string) {
	var l Language
	ok := false
	for _, lang := range languages {
		if lang.Match(fname) {
			ok = true
			l = lang
			break
		}
	}
	if !ok {
		return // ignore this file
	}
	i, ok := info[l.Name()]
	if !ok {
		i = &Stats{}
		info[l.Name()] = i
	}
	c, err := ioutil.ReadFile(fname)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ! %s\n", fname)
		return
	}
	l.Update(c, i)
}

var files []string

func add(n string) {
	fi, err := os.Stat(n)
	if err != nil {
		goto invalid
	}
	if fi.IsDir() {
		fs, err := ioutil.ReadDir(n)
		if err != nil {
			goto invalid
		}
		for _, f := range fs {
			add(path.Join(n, f.Name()))
		}
		return
	}
	if fi.Mode()&os.ModeType == 0 {
		files = append(files, n)
		return
	}

	println(fi.Mode())

invalid:
	fmt.Fprintf(os.Stderr, "  ! %s\n", n)
}

type LData []LResult

func (d LData) Len() int { return len(d) }

func (d LData) Less(i, j int) bool {
	if d[i].CodeLines == d[j].CodeLines {
		return d[i].Name > d[j].Name
	}
	return d[i].CodeLines > d[j].CodeLines
}

func (d LData) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}

type LResult struct {
	Name         string
	FileCount    int
	CodeLines    int
	CommentLines int
	BlankLines   int
	TotalLines   int
}

func (r *LResult) Add(a LResult) {
	r.FileCount += a.FileCount
	r.CodeLines += a.CodeLines
	r.CommentLines += a.CommentLines
	r.BlankLines += a.BlankLines
	r.TotalLines += a.TotalLines
}

func printJSON() {
	bs, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(bs))
}

func printInfo() {
	w := tabwriter.NewWriter(os.Stdout, 2, 8, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "Language\tFiles\tCode\tComment\tBlank\tTotal\t")
	d := LData([]LResult{})
	total := &LResult{}
	total.Name = "Total"
	for n, i := range info {
		r := LResult{
			n,
			i.FileCount,
			i.CodeLines,
			i.CommentLines,
			i.BlankLines,
			i.TotalLines,
		}
		d = append(d, r)
		total.Add(r)
	}
	d = append(d, *total)
	sort.Sort(d)
	//d[0].Name = "Total"
	for _, i := range d {
		fmt.Fprintf(
			w,
			"%s\t%d\t%d\t%d\t%d\t%d\t\n",
			i.Name,
			i.FileCount,
			i.CodeLines,
			i.CommentLines,
			i.BlankLines,
			i.TotalLines)
	}

	w.Flush()
}

var (
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
	useJson    = flag.Bool("json", false, "JSON-format output")
	version    = flag.Bool("V", false, "display version info and exit")
)

func main() {
	flag.Parse()
	if *version {
		fmt.Printf("sloc %s\n", VERSION)
		return
	}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	args := flag.Args()
	if len(args) == 0 {
		args = append(args, `.`)
	}

	for _, n := range args {
		add(n)
	}

	for _, f := range files {
		handleFile(f)
	}

	if *useJson {
		printJSON()
	} else {
		printInfo()
	}
}
