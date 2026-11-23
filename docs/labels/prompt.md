You are the second, independent rater in a mutation-testing study of Go libraries. Below are 50 mutants that SURVIVED: they compile and no test in the library's suite fails on them. For each, choose exactly one label:

- equivalent: no input or call sequence can make the mutated program behave differently from the original (same results, same errors, no new panics). Performance-only differences do NOT count as behavior.
- useful: behavior can differ, and a reasonable maintainer would write or strengthen a test to catch it (a real gap in the tests: a boundary, an error path a caller can reach, untested API).
- unproductive: behavior can differ, but a maintainer would not write a test for it: performance-only or optimization changes visible only in speed, defensive checks for states the code never reaches, unreachable error paths, debug or log output.

The line marked with > is the mutated line. "_ = x" or "func() bool { _ = x; return true }()" only keeps a variable used so the mutant compiles; read it as the plain replacement (true, false, nil, deletion). Judge from the code shown and your knowledge of Go; if unsure, pick the more likely label and say why in the reason.

Answer with every key exactly once, in order, each with a one-sentence reason naming the concrete input or argument.

### glob#1153  lexer.go:329  operator ROR  reached by some test: yes
change: `r == char_range_not`  →  `r <= char_range_not`
```go
 321  		}
 322  
 323  		if wantHi {
 324  			l.tokens.push(Token{RangeHi, string(r)})
 325  			wantClose = true
 326  			continue
 327  		}
 328  
 329> 		if !seenNot && r == char_range_not {
 330  			l.tokens.push(Token{Not, string(r)})
 331  			seenNot = true
 332  			continue
 333  		}
 334  
 335  		if n, w := l.peek(); n == char_range_between {
 336  			l.seek(w)
 337  			l.tokens.push(Token{RangeLo, string(r)})
```

### glob#867  parse.go:660  operator COR  reached by some test: yes
change: `ok && star.SepStr == ""`  →  `func() bool { _, _ = ok, star; return false }()`
```go
 652  				folded = &prefixSuffixMatcher{
 653  					Prefix: t.Text,
 654  					Suffix: last.Text,
 655  					Sep:    last.Sep,
 656  				}
 657  			}
 658  
 659  		case *containsMatcher:
 660> 			if star, ok := prev.(*starMatcher); ok && star.SepStr == "" {
 661  				folded = last
 662  			}
 663  		}
 664  		if folded == nil {
 665  			break
 666  		}
 667  		ms = ms[:len(ms)-1]
 668  		ms[len(ms)-1] = folded
```

### glob#955  parse.go:808  operator COR  reached by some test: yes
change: `i < len(a) && i < len(b)`  →  `i < len(a) || i < len(b)`
```go
 800  	}
 801  	return "" // A star, a single-character matcher or a void.
 802  }
 803  
 804  // commonSuffix returns the longest common suffix of a and b, never splitting
 805  // a multi-byte rune.
 806  func commonSuffix(a, b string) string {
 807  	i := 0
 808> 	for i < len(a) && i < len(b) {
 809  		ra, wa := utf8.DecodeLastRuneInString(a[:len(a)-i])
 810  		rb, wb := utf8.DecodeLastRuneInString(b[:len(b)-i])
 811  		if ra != rb || wa != wb {
 812  			break
 813  		}
 814  		i += wa
 815  	}
 816  	return a[len(a)-i:]
```

### glob#905  parse.go:695  operator SDL  reached by some test: yes
change: `star.Next = leadingLiteral(v[i+1])`  →  `(statement deleted)`
```go
 687  			last := i == len(v)-1
 688  			star, ok := c.(*starMatcher)
 689  			if !ok {
 690  				annotateStars(c, tail && last)
 691  				continue
 692  			}
 693  			star.Terminal = tail && last
 694  			if !last {
 695> 				star.Next = leadingLiteral(v[i+1])
 696  			}
 697  		}
 698  	case altMatcher:
 699  		for _, c := range v {
 700  			annotateStars(c, tail)
 701  		}
 702  	case *starMatcher:
 703  		v.Terminal = tail
```

### glob#734  parse.go:466  operator AOR  reached by some test: yes
change: `i-1`  →  `i/1`
```go
 458  		case *textMatcher:
 459  			if i > 0 {
 460  				if _, ok := ms[i-1].(*textMatcher); ok {
 461  					return true
 462  				}
 463  			}
 464  		case *starMatcher:
 465  			if i > 0 {
 466> 				if _, ok := ms[i-1].(*starMatcher); ok {
 467  					return true
 468  				}
 469  			}
 470  		}
 471  	}
 472  	return false
 473  }
 474  
```

### glob#1053  lexer.go:232  operator SDL  reached by some test: yes
change: `l.errorf("could not unread rune")`  →  `(statement deleted)`
```go
 224  }
 225  
 226  func (l *Lexer) seek(w int) {
 227  	l.pos += w
 228  }
 229  
 230  func (l *Lexer) unread() {
 231  	if l.hasRune {
 232> 		l.errorf("could not unread rune")
 233  		return
 234  	}
 235  	l.seek(-l.lastRuneSize)
 236  	l.hasRune = true
 237  }
 238  
 239  func (l *Lexer) errorf(f string, v ...any) {
 240  	l.err = fmt.Errorf(f, v...)
```

### glob#932  parse.go:763  operator AOR  reached by some test: yes
change: `n += minLength(c)`  →  `n -= minLength(c)`
```go
 755  	case *suffixMatcher:
 756  		return len(v.Text)
 757  	case *prefixSuffixMatcher:
 758  		return len(v.Prefix) + len(v.Suffix)
 759  	case *containsMatcher:
 760  		return len(v.Text)
 761  	case multiMatcher:
 762  		for _, c := range v {
 763> 			n += minLength(c)
 764  		}
 765  		return n
 766  	case tailAltMatcher:
 767  		return minLength(altMatcher(v))
 768  	case altMatcher:
 769  		n = minLength(v[0])
 770  		for _, c := range v[1:] {
 771  			n = min(n, minLength(c))
```

### glob#3  glob.go:152  operator COR  reached by some test: yes
change: `err != nil`  →  `false`
```go
 144  	}
 145  	return c.Compile(pattern)
 146  }
 147  
 148  // MustCompile is the same as Compile, except that if Compile returns error,
 149  // this will panic.
 150  func MustCompile(pattern string, separators ...rune) *Pattern {
 151  	g, err := Compile(pattern, separators...)
 152> 	if err != nil {
 153  		panic(err)
 154  	}
 155  	return g
 156  }
 157  
 158  // Compiler holds the options a pattern is compiled with; see
 159  // [Compiler.Compile].
 160  //
```

### glob#353  match.go:601  operator COR  reached by some test: yes
change: `!slices.Contains(m.Sep, r)`  →  `func() bool { _ = r; return true }()`
```go
 593  	if len(s) == 0 {
 594  		return 0, true
 595  	}
 596  	if m.Next != "" {
 597  		m.storeSkip(x, s)
 598  		return 0, true
 599  	}
 600  	r, n := utf8.DecodeRuneInString(s)
 601> 	if !slices.Contains(m.Sep, r) {
 602  		// The star may extend over the rune: save the restart point past
 603  		// it. A separator-free star (`**`) can extend over anything the
 604  		// pending restart points could reach, so it may discard them; see
 605  		// [matchContext.storeStar].
 606  		x.storeStar(n, len(m.Sep) == 0)
 607  	}
 608  	return 0, true
 609  }
```

### go-humanize#1093  words.go:52  operator AOR  reached by some test: yes
change: `l-2`  →  `l/2`
```go
  44  	// Reference: http://en.wikipedia.org/wiki/English_plural
  45  
  46  	for _, ending := range sibilantEndings {
  47  		if strings.HasSuffix(singular, ending) {
  48  			return singular + "es"
  49  		}
  50  	}
  51  	l := len(singular)
  52> 	if l >= 2 && singular[l-1] == 'o' && !isVowel[singular[l-2]] {
  53  		return singular + "es"
  54  	}
  55  	if l >= 2 && singular[l-1] == 'y' && !isVowel[singular[l-2]] {
  56  		return singular[:l-1] + "ies"
  57  	}
  58  
  59  	return singular + "s"
  60  }
```

### go-humanize#527  commaf.go:16  operator ROR  reached by some test: yes
change: `v.Sign() < 0`  →  `v.Sign() <= 0`
```go
   8  	"math/big"
   9  	"strings"
  10  )
  11  
  12  // BigCommaf produces a string form of the given big.Float in base 10
  13  // with commas after every three orders of magnitude.
  14  func BigCommaf(v *big.Float) string {
  15  	if v.IsInf() {
  16> 		if v.Sign() < 0 {
  17  			return "-Inf"
  18  		}
  19  		return "+Inf"
  20  	}
  21  
  22  	buf := &bytes.Buffer{}
  23  	if v.Sign() < 0 {
  24  		buf.Write([]byte{'-'})
```

### go-humanize#263  bytes.go:200  operator COR  reached by some test: yes
change: `perr == nil`  →  `func() bool { _ = perr; return true }()`
```go
 192  
 193  	extra := strings.ToLower(strings.TrimSpace(s[lastDigit:]))
 194  	if m, ok := bytesSizeTable[extra]; ok {
 195  		// Parse whole-number inputs exactly. A float64 cannot represent
 196  		// integers above 2^53, and the multiplication below would also reject
 197  		// otherwise-valid values near math.MaxUint64, so route integers through
 198  		// uint64 arithmetic with an overflow check.
 199  		if !strings.ContainsRune(num, '.') {
 200> 			if v, perr := strconv.ParseUint(num, 10, 64); perr == nil {
 201  				if hi, lo := bits.Mul64(v, m); hi == 0 {
 202  					return lo, nil
 203  				}
 204  				return 0, fmt.Errorf("too large: %v", s)
 205  			}
 206  		}
 207  		f *= float64(m)
 208  		if f >= math.MaxUint64 {
```

### go-humanize#214  bytes.go:104  operator ROR  reached by some test: yes
change: `int(e)+1 < len(sizes)`  →  `int(e)+1 <= len(sizes)`
```go
  96  	// 9.996 -> 10.0), which means fewer decimal places should be shown
  97  	// than originally computed.
  98  	if newIntDigits := countDigits(int64(val)); newIntDigits > intDigits {
  99  		digits = max(digits-(newIntDigits-intDigits), 0)
 100  	}
 101  
 102  	// A rounded value may now fit the next unit (1000 kB -> 1.0 MB).
 103  	// Recompute the precision for that unit without rounding the input again.
 104> 	if val >= base && int(e)+1 < len(sizes) {
 105  		e++
 106  		val /= base
 107  		digits = max(minDigits-countDigits(int64(val)), 0)
 108  	}
 109  	suffix := sizes[int(e)]
 110  
 111  	ff := "%%.%df %%s"
 112  	f := fmt.Sprintf(ff, digits)
```

### go-humanize#42  bigbytes.go:113  operator ROR  reached by some test: yes
change: `s.Cmp(ten) < 0`  →  `s.Cmp(ten) <= 0`
```go
 105  	"ri": BigRiByte,
 106  	"q":  BigQByte,
 107  	"qi": BigQiByte,
 108  }
 109  
 110  var ten = big.NewInt(10)
 111  
 112  func humanateBigBytes(s, base *big.Int, sizes []string) string {
 113> 	if s.Cmp(ten) < 0 {
 114  		return fmt.Sprintf("%d B", s)
 115  	}
 116  	c := (&big.Int{}).Set(s)
 117  	val, mag := oomm(c, base, len(sizes)-1)
 118  	// Values this close to the next unit round up when formatted without
 119  	// decimals. Promote the suffix too, unless it is already the last one.
 120  	if val >= float64(base.Int64())-0.5 && mag+1 < len(sizes) {
 121  		val = 1
```

### go-humanize#433  comma.go:90  operator ROR  reached by some test: yes
change: `pos < len(parts[0])`  →  `pos != len(parts[0])`
```go
  82  
  83  	parts := strings.Split(strconv.FormatFloat(v, 'f', -1, 64), ".")
  84  	pos := 0
  85  	if len(parts[0])%3 != 0 {
  86  		pos += len(parts[0]) % 3
  87  		buf.WriteString(parts[0][:pos])
  88  		buf.Write(comma)
  89  	}
  90> 	for ; pos < len(parts[0]); pos += 3 {
  91  		buf.WriteString(parts[0][pos : pos+3])
  92  		buf.Write(comma)
  93  	}
  94  	buf.Truncate(buf.Len() - 1)
  95  
  96  	if len(parts) > 1 {
  97  		buf.Write([]byte{'.'})
  98  		buf.WriteString(parts[1])
```

### go-humanize#785  number.go:167  operator ROR  reached by some test: yes
change: `len(thousandStr) > 0`  →  `len(thousandStr) != 0`
```go
 159  
 160  	// split number into integer and fractional parts
 161  	intf, fracf := math.Modf(n + renderFloatPrecisionRounders[precision])
 162  
 163  	// generate integer part string
 164  	intStr := strconv.FormatFloat(intf, 'f', 0, 64)
 165  
 166  	// add thousand separator if required
 167> 	if len(thousandStr) > 0 {
 168  		for i := len(intStr); i > 3; {
 169  			i -= 3
 170  			intStr = intStr[:i] + thousandStr + intStr[i:]
 171  		}
 172  	}
 173  
 174  	// no fractional part, we can leave now
 175  	if precision == 0 {
```

### go-humanize#386  comma.go:54  operator ROR  reached by some test: yes
change: `j == 1`  →  `j >= 1`
```go
  46  			output[j] = ','
  47  			j--
  48  		} else {
  49  			counter++
  50  		}
  51  	}
  52  
  53  	output[j] = byte(v) + 48
  54> 	if j == 1 {
  55  		output[0] = '-'
  56  	}
  57  	return string(output)
  58  }
  59  
  60  // Commaf produces a string form of the given number in base 10 with
  61  // commas after every three orders of magnitude.
  62  //
```

### go-humanize#664  number.go:77  operator ROR  reached by some test: yes
change: `n < (0.0 - math.MaxFloat64)`  →  `n <= (0.0 - math.MaxFloat64)`
```go
  69  	//   +Inf = "+Infinity"
  70  	//   -Inf = "-Infinity"
  71  	if math.IsNaN(n) {
  72  		return "NaN"
  73  	}
  74  	if n > math.MaxFloat64 {
  75  		return "Infinity"
  76  	}
  77> 	if n < (0.0 - math.MaxFloat64) {
  78  		return "-Infinity"
  79  	}
  80  
  81  	// default format
  82  	precision := 2
  83  	decimalStr := "."
  84  	thousandStr := ","
  85  	positiveStr := ""
```

### go-version#86  constraint.go:253  operator ROR  reached by some test: yes
change: `v.Compare(c) == 1`  →  `v.Compare(c) <= 1`
```go
 245  	return v.Equal(c)
 246  }
 247  
 248  func constraintNotEqual(v, c *Version) bool {
 249  	return !v.Equal(c)
 250  }
 251  
 252  func constraintGreaterThan(v, c *Version) bool {
 253> 	return prereleaseCheck(v, c) && v.Compare(c) == 1
 254  }
 255  
 256  func constraintLessThan(v, c *Version) bool {
 257  	return prereleaseCheck(v, c) && v.Compare(c) == -1
 258  }
 259  
 260  func constraintGreaterThanEqual(v, c *Version) bool {
 261  	return prereleaseCheck(v, c) && v.Compare(c) >= 0
```

### go-version#118  constraint.go:270  operator ROR  reached by some test: yes
change: `v.Prerelease() == ""`  →  `v.Prerelease() <= ""`
```go
 262  }
 263  
 264  func constraintLessThanEqual(v, c *Version) bool {
 265  	return prereleaseCheck(v, c) && v.Compare(c) <= 0
 266  }
 267  
 268  func constraintPessimistic(v, c *Version) bool {
 269  	// Using a pessimistic constraint with a pre-release, restricts versions to pre-releases
 270> 	if !prereleaseCheck(v, c) || (c.Prerelease() != "" && v.Prerelease() == "") {
 271  		return false
 272  	}
 273  
 274  	// If the version being checked is naturally less than the constraint, then there
 275  	// is no way for the version to be valid against the constraint
 276  	if v.LessThan(c) {
 277  		return false
 278  	}
```

### go-version#107  constraint.go:265  operator ROR  reached by some test: yes
change: `v.Compare(c) <= 0`  →  `v.Compare(c) != 0`
```go
 257  	return prereleaseCheck(v, c) && v.Compare(c) == -1
 258  }
 259  
 260  func constraintGreaterThanEqual(v, c *Version) bool {
 261  	return prereleaseCheck(v, c) && v.Compare(c) >= 0
 262  }
 263  
 264  func constraintLessThanEqual(v, c *Version) bool {
 265> 	return prereleaseCheck(v, c) && v.Compare(c) <= 0
 266  }
 267  
 268  func constraintPessimistic(v, c *Version) bool {
 269  	// Using a pessimistic constraint with a pre-release, restricts versions to pre-releases
 270  	if !prereleaseCheck(v, c) || (c.Prerelease() != "" && v.Prerelease() == "") {
 271  		return false
 272  	}
 273  
```

### go-version#518  version.go:483  operator ROR  reached by some test: yes
change: `v.pre != ""`  →  `v.pre > ""`
```go
 475  	}
 476  	for i, s := range segments {
 477  		if i > 0 {
 478  			buf = append(buf, '.')
 479  		}
 480  		buf = strconv.AppendInt(buf, s, 10)
 481  	}
 482  
 483> 	if v.pre != "" {
 484  		buf = append(buf, '-')
 485  		buf = append(buf, v.pre...)
 486  	}
 487  
 488  	if v.metadata != "" {
 489  		buf = append(buf, '+')
 490  		buf = append(buf, v.metadata...)
 491  	}
```

### go-version#123  constraint.go:276  operator COR  reached by some test: yes
change: `v.LessThan(c)`  →  `false`
```go
 268  func constraintPessimistic(v, c *Version) bool {
 269  	// Using a pessimistic constraint with a pre-release, restricts versions to pre-releases
 270  	if !prereleaseCheck(v, c) || (c.Prerelease() != "" && v.Prerelease() == "") {
 271  		return false
 272  	}
 273  
 274  	// If the version being checked is naturally less than the constraint, then there
 275  	// is no way for the version to be valid against the constraint
 276> 	if v.LessThan(c) {
 277  		return false
 278  	}
 279  	// We'll use this more than once, so grab the length now so it's a little cleaner
 280  	// to write the later checks
 281  	cs := len(c.segments)
 282  
 283  	// If the version being checked has less specificity than the constraint, then there
 284  	// is no way for the version to be valid against the constraint
```

### go-version#263  version.go:204  operator ROR  reached by some test: yes
change: `i < hS`  →  `i != hS`
```go
 196  	lenOther := len(segmentsOther)
 197  	hS := lenSelf
 198  	if lenSelf < lenOther {
 199  		hS = lenOther
 200  	}
 201  	// Compare the segments
 202  	// Because a constraint could have more/less specificity than the version it's
 203  	// checking, we need to account for a lopsided or jagged comparison
 204> 	for i := 0; i < hS; i++ {
 205  		if i > lenSelf-1 {
 206  			// This means Self had the lower specificity
 207  			// Check to see if the remaining segments in Other are all zeros
 208  			if !allZero(segmentsOther[i:]) {
 209  				// if not, it means that Other has to be greater than Self
 210  				return -1
 211  			}
 212  			break
```

### go-version#342  version.go:270  operator ROR  reached by some test: yes
change: `c > '9'`  →  `c >= '9'`
```go
 262  // Per the semver spec, numeric identifiers must not have leading zeroes,
 263  // so a longer string always represents a larger value and lexicographic
 264  // order equals numeric order for strings of the same length.
 265  func isNumericIdentifier(s string) bool {
 266  	if s == "" {
 267  		return false
 268  	}
 269  	for _, c := range s {
 270> 		if c < '0' || c > '9' {
 271  			return false
 272  		}
 273  	}
 274  	return true
 275  }
 276  
 277  // compareNumericIdentifiers compares two all-digit strings by value without
 278  // parsing them into a fixed-width integer type, correctly handling values
```

### go-version#4  constraint.go:41  operator ROR  reached by some test: yes
change: `c.op == con.op`  →  `c.op <= con.op`
```go
  33  type Constraint struct {
  34  	f        constraintFunc
  35  	op       operator
  36  	check    *Version
  37  	original string
  38  }
  39  
  40  func (c *Constraint) Equals(con *Constraint) bool {
  41> 	return c.op == con.op && c.check.Equal(con.check)
  42  }
  43  
  44  // Constraints is a slice of constraints. We make a custom type so that
  45  // we can add methods to it.
  46  type Constraints []*Constraint
  47  
  48  type constraintFunc func(v, c *Version) bool
  49  
```

### go-version#167  version.go:86  operator COR  reached by some test: yes
change: `opt != nil`  →  `true`
```go
  78  
  79  // NewVersion parses the given version and returns a new Version.
  80  //
  81  // Optional parsing behavior can be enabled with Option values such as
  82  // WithPrefix, which validates and strips an expected prefix before parsing.
  83  func NewVersion(v string, opts ...Option) (*Version, error) {
  84  	options := &options{}
  85  	for _, opt := range opts {
  86> 		if opt != nil {
  87  			opt(options)
  88  		}
  89  	}
  90  
  91  	vToParse := v
  92  	if options.prefix != "" {
  93  		if !strings.HasPrefix(v, options.prefix) {
  94  			return nil, fmt.Errorf("version %q does not have prefix %q", v, options.prefix)
```

### semver#501  constraints.go:613  operator COR  reached by some test: yes
change: `m == nil`  →  `false`
```go
 605  		return true
 606  	default:
 607  		return false
 608  	}
 609  }
 610  
 611  func rewriteRange(i string) string {
 612  	m := constraintRangeRegex.FindAllStringSubmatch(i, -1)
 613> 	if m == nil {
 614  		return i
 615  	}
 616  	o := i
 617  	for _, v := range m {
 618  		t := fmt.Sprintf(">= %s, <= %s ", v[1], v[11])
 619  		o = strings.Replace(o, v[0], t, 1)
 620  	}
 621  
```

### semver#582  version.go:183  operator ROR  reached by some test: yes
change: `len(v) > MaxVersionLen`  →  `len(v) >= MaxVersionLen`
```go
 175  	return sv, nil
 176  }
 177  
 178  // NewVersion parses a given version and returns an instance of Version or
 179  // an error if unable to parse the version. If the version is SemVer-ish it
 180  // attempts to convert it to SemVer. If you want  to validate it was a strict
 181  // semantic version at parse time see StrictNewVersion().
 182  func NewVersion(v string) (*Version, error) {
 183> 	if len(v) > MaxVersionLen {
 184  		return nil, ErrVersionTooLong
 185  	}
 186  	if CoerceNewVersion {
 187  		return coerceNewVersion(v)
 188  	}
 189  	m := versionRegex.FindStringSubmatch(v)
 190  	if m == nil {
 191  
```

### semver#1043  version.go:750  operator ROR  reached by some test: yes
change: `s != ""`  →  `s > ""`
```go
 742  	if s == "" {
 743  		if o != "" {
 744  			return -1
 745  		}
 746  		return 1
 747  	}
 748  
 749  	if o == "" {
 750> 		if s != "" {
 751  			return 1
 752  		}
 753  		return -1
 754  	}
 755  
 756  	// When comparing strings "99" is greater than "103". To handle
 757  	// cases like this we need to detect numbers and compare them. According
 758  	// to the semver spec, numbers are always positive. If there is a - at the
```

### semver#55  constraints.go:130  operator COR  reached by some test: yes
change: `!prerelesase`  →  `func() bool { _ = prerelesase; return true }()`
```go
 122  	// this var is marked
 123  	var prerelesase bool
 124  	for i, o := range cs.constraints {
 125  		joy := true
 126  		for _, c := range o {
 127  			// Before running the check handle the case there the version is
 128  			// a prerelease and the check is not searching for prereleases.
 129  			if !cs.IncludePrerelease && !cs.containsPre[i] && v.pre != "" {
 130> 				if !prerelesase {
 131  					em := fmt.Errorf("%q is a prerelease version and the constraint is only looking for release versions", v)
 132  					e = append(e, em)
 133  					prerelesase = true
 134  				}
 135  				joy = false
 136  
 137  			} else {
 138  
```

### semver#193  constraints.go:365  operator COR  reached by some test: yes
change: `v.Prerelease() != "" || c.con.Prerelease() != ""`  →  `true`
```go
 357  		if c.con.Minor() != v.Minor() && !c.minorDirty {
 358  			return true, nil
 359  		} else if c.minorDirty {
 360  			return false, fmt.Errorf("%q is equal to %q", v, c.orig)
 361  		} else if c.con.Patch() != v.Patch() && !c.patchDirty {
 362  			return true, nil
 363  		} else if c.patchDirty {
 364  			// Need to handle prereleases if present
 365> 			if v.Prerelease() != "" || c.con.Prerelease() != "" {
 366  				eq := comparePrerelease(v.Prerelease(), c.con.Prerelease()) != 0
 367  				if eq {
 368  					return true, nil
 369  				}
 370  				return false, fmt.Errorf("%q is equal to %q", v, c.orig)
 371  			}
 372  			return false, fmt.Errorf("%q is equal to %q", v, c.orig)
 373  		}
```

### semver#626  version.go:231  operator ROR  reached by some test: yes
change: `m[3] != ""`  →  `m[3] > ""`
```go
 223  		sv.minor, err = strconv.ParseUint(m[2], 10, 64)
 224  		if err != nil {
 225  			return nil, fmt.Errorf("error parsing version segment: %w", err)
 226  		}
 227  	} else {
 228  		sv.minor = 0
 229  	}
 230  
 231> 	if m[3] != "" {
 232  		sv.patch, err = strconv.ParseUint(m[3], 10, 64)
 233  		if err != nil {
 234  			return nil, fmt.Errorf("error parsing version segment: %w", err)
 235  		}
 236  	} else {
 237  		sv.patch = 0
 238  	}
 239  
```

### semver#466  constraints.go:581  operator ROR  reached by some test: yes
change: `c.con.Minor() > 0`  →  `c.con.Minor() != 0`
```go
 573  		return false, fmt.Errorf("%q does not have same major version as %q", v, c.orig)
 574  	}
 575  
 576  	// ^ when the major is 0 and minor > 0 is >=0.y.z < 0.y+1
 577  	if c.con.Major() == 0 && v.Major() > 0 {
 578  		return false, fmt.Errorf("%q does not have same major version as %q", v, c.orig)
 579  	}
 580  	// If the con Minor is > 0 it is not dirty
 581> 	if c.con.Minor() > 0 || c.patchDirty {
 582  		eq = v.Minor() == c.con.Minor()
 583  		if eq {
 584  			return true, nil
 585  		}
 586  		return false, fmt.Errorf("%q does not have same minor version as %q. Expected minor versions to match when constraint major version is 0", v, c.orig)
 587  	}
 588  	// ^ when the minor is 0 and minor > 0 is =0.0.z
 589  	if c.con.Minor() == 0 && v.Minor() > 0 {
```

### semver#572  version.go:167  operator ERR  reached by some test: yes
change: `return nil, err`  →  `return nil, nil`
```go
 159  	var err error
 160  	sv.major, err = strconv.ParseUint(parts[0], 10, 64)
 161  	if err != nil {
 162  		return nil, err
 163  	}
 164  
 165  	sv.minor, err = strconv.ParseUint(parts[1], 10, 64)
 166  	if err != nil {
 167> 		return nil, err
 168  	}
 169  
 170  	sv.patch, err = strconv.ParseUint(parts[2], 10, 64)
 171  	if err != nil {
 172  		return nil, err
 173  	}
 174  
 175  	return sv, nil
```

### semver#1214  version.go:873  operator ERR  reached by some test: yes
change: `err != nil`  →  `err == nil`
```go
 865  
 866  	if m[5] != "" {
 867  		if err = validatePrerelease(m[5]); err != nil {
 868  			return err
 869  		}
 870  	}
 871  
 872  	if m[8] != "" {
 873> 		if err = validateMetadata(m[8]); err != nil {
 874  			return err
 875  		}
 876  	}
 877  
 878  	return nil
 879  }
```

### shlex#52  shlex.go:252  operator ERR  reached by some test: yes
change: `return token, err`  →  `return token, nil`
```go
 244  		case inWordState: // in a regular word
 245  			{
 246  				switch nextRuneType {
 247  				case eofRuneClass:
 248  					{
 249  						token := &Token{
 250  							tokenType: tokenType,
 251  							value:     string(value)}
 252> 						return token, err
 253  					}
 254  				case spaceRuneClass:
 255  					{
 256  						token := &Token{
 257  							tokenType: tokenType,
 258  							value:     string(value)}
 259  						return token, err
 260  					}
```

### shlex#62  shlex.go:302  operator SDL  reached by some test: yes
change: `err = fmt.Errorf("EOF found after escape character")`  →  `(statement deleted)`
```go
 294  					}
 295  				}
 296  			}
 297  		case escapingQuotedState: // the next rune after an escape character, in double quotes
 298  			{
 299  				switch nextRuneType {
 300  				case eofRuneClass:
 301  					{
 302> 						err = fmt.Errorf("EOF found after escape character")
 303  						token := &Token{
 304  							tokenType: tokenType,
 305  							value:     string(value)}
 306  						return token, err
 307  					}
 308  				default:
 309  					{
 310  						state = quotingEscapingState
```

### shlex#56  shlex.go:271  operator SDL  reached by some test: yes
change: `state = escapingState`  →  `(statement deleted)`
```go
 263  						state = quotingEscapingState
 264  					}
 265  				case nonEscapingQuoteRuneClass:
 266  					{
 267  						state = quotingState
 268  					}
 269  				case escapeRuneClass:
 270  					{
 271> 						state = escapingState
 272  					}
 273  				default:
 274  					{
 275  						value = append(value, nextRune)
 276  					}
 277  				}
 278  			}
 279  		case escapingState: // the rune after an escape character
```

### shlex#7  shlex.go:71  operator COR  reached by some test: yes
change: `a.tokenType != b.tokenType`  →  `false`
```go
  63  
  64  // Equal reports whether tokens a, and b, are equal.
  65  // Two tokens are equal if both their types and values are equal. A nil token can
  66  // never be equal to another token.
  67  func (a *Token) Equal(b *Token) bool {
  68  	if a == nil || b == nil {
  69  		return false
  70  	}
  71> 	if a.tokenType != b.tokenType {
  72  		return false
  73  	}
  74  	return a.value == b.value
  75  }
  76  
  77  // Named classes of UTF-8 runes
  78  const (
  79  	spaceRunes            = " \t\r\n"
```

### shlex#55  shlex.go:267  operator SDL  reached by some test: yes
change: `state = quotingState`  →  `(statement deleted)`
```go
 259  						return token, err
 260  					}
 261  				case escapingQuoteRuneClass:
 262  					{
 263  						state = quotingEscapingState
 264  					}
 265  				case nonEscapingQuoteRuneClass:
 266  					{
 267> 						state = quotingState
 268  					}
 269  				case escapeRuneClass:
 270  					{
 271  						state = escapingState
 272  					}
 273  				default:
 274  					{
 275  						value = append(value, nextRune)
```

### uuid#338  uuid.go:127  operator ROR  reached by some test: yes
change: `s[18] != '-'`  →  `s[18] > '-'`
```go
 119  			}
 120  		}
 121  		return uuid, nil
 122  	default:
 123  		return uuid, invalidLengthError{len(s)}
 124  	}
 125  	// s is now at least 36 bytes long
 126  	// it must be of the form  xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
 127> 	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
 128  		return uuid, ErrInvalidUUIDFormat
 129  
 130  	}
 131  	for i, x := range [16]int{
 132  		0, 2, 4, 6,
 133  		9, 11,
 134  		14, 16,
 135  		19, 21,
```

### uuid#113  null.go:71  operator ROR  reached by some test: no
change: `len(data) != 16`  →  `len(data) == 16`
```go
  63  		return nu.UUID[:], nil
  64  	}
  65  
  66  	return []byte(nil), nil
  67  }
  68  
  69  // UnmarshalBinary implements encoding.BinaryUnmarshaler.
  70  func (nu *NullUUID) UnmarshalBinary(data []byte) error {
  71> 	if len(data) != 16 {
  72  		return fmt.Errorf("invalid UUID (got %d bytes)", len(data))
  73  	}
  74  	copy(nu.UUID[:], data)
  75  	nu.Valid = true
  76  	return nil
  77  }
  78  
  79  // MarshalText implements encoding.TextMarshaler.
```

### uuid#110  null.go:71  operator ROR  reached by some test: no
change: `len(data) != 16`  →  `len(data) <= 16`
```go
  63  		return nu.UUID[:], nil
  64  	}
  65  
  66  	return []byte(nil), nil
  67  }
  68  
  69  // UnmarshalBinary implements encoding.BinaryUnmarshaler.
  70  func (nu *NullUUID) UnmarshalBinary(data []byte) error {
  71> 	if len(data) != 16 {
  72  		return fmt.Errorf("invalid UUID (got %d bytes)", len(data))
  73  	}
  74  	copy(nu.UUID[:], data)
  75  	nu.Valid = true
  76  	return nil
  77  }
  78  
  79  // MarshalText implements encoding.TextMarshaler.
```

### uuid#588  version1.go:38  operator SDL  reached by some test: yes
change: `setNodeInterface("")`  →  `(statement deleted)`
```go
  30  
  31  	binary.BigEndian.PutUint32(uuid[0:], timeLow)
  32  	binary.BigEndian.PutUint16(uuid[4:], timeMid)
  33  	binary.BigEndian.PutUint16(uuid[6:], timeHi)
  34  	binary.BigEndian.PutUint16(uuid[8:], seq)
  35  
  36  	nodeMu.Lock()
  37  	if nodeID == zeroID {
  38> 		setNodeInterface("")
  39  	}
  40  	copy(uuid[10:], nodeID[:])
  41  	nodeMu.Unlock()
  42  
  43  	return uuid, nil
  44  }
```

### uuid#76  node_net.go:28  operator ROR  reached by some test: yes
change: `len(ifs.HardwareAddr) >= 6`  →  `len(ifs.HardwareAddr) < 6`
```go
  20  	if interfaces == nil {
  21  		var err error
  22  		interfaces, err = net.Interfaces()
  23  		if err != nil {
  24  			return "", nil
  25  		}
  26  	}
  27  	for _, ifs := range interfaces {
  28> 		if len(ifs.HardwareAddr) >= 6 && (name == "" || name == ifs.Name) {
  29  			return ifs.Name, ifs.HardwareAddr
  30  		}
  31  	}
  32  	return "", nil
  33  }
```

### uuid#649  version7.go:26  operator ERR  reached by some test: yes
change: `return uuid, err`  →  `return uuid, nil`
```go
  18  // Implementations SHOULD utilize UUID version 7 over UUID version 1 and 6 if possible.
  19  //
  20  // NewV7 returns a Version 7 UUID based on the current time(Unix Epoch).
  21  // Uses the randomness pool if it was enabled with EnableRandPool.
  22  // On error, NewV7 returns Nil and an error
  23  func NewV7() (UUID, error) {
  24  	uuid, err := NewRandom()
  25  	if err != nil {
  26> 		return uuid, err
  27  	}
  28  	makeV7(uuid[:])
  29  	return uuid, nil
  30  }
  31  
  32  // NewV7FromReader returns a Version 7 UUID based on the current time(Unix Epoch).
  33  // it use NewRandomFromReader fill random bits.
  34  // On error, NewV7FromReader returns Nil and an error.
```

### uuid#656  version7.go:62  operator SDL  reached by some test: yes
change: `_ = uuid[15]`  →  `(statement deleted)`
```go
  54  		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
  55  		|          unix_ts_ms           |  ver  |  rand_a (12 bit seq)  |
  56  		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
  57  		|var|                        rand_b                             |
  58  		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
  59  		|                            rand_b                             |
  60  		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
  61  	*/
  62> 	_ = uuid[15] // bounds check
  63  
  64  	t, s := getV7Time()
  65  
  66  	uuid[0] = byte(t >> 40)
  67  	uuid[1] = byte(t >> 32)
  68  	uuid[2] = byte(t >> 24)
  69  	uuid[3] = byte(t >> 16)
  70  	uuid[4] = byte(t >> 8)
```

### uuid#564  uuid.go:319  operator ROR  reached by some test: no
change: `v > 15`  →  `v != 15`
```go
 311  }
 312  
 313  // Version returns the version of uuid.
 314  func (uuid UUID) Version() Version {
 315  	return Version(uuid[6] >> 4)
 316  }
 317  
 318  func (v Version) String() string {
 319> 	if v > 15 {
 320  		return fmt.Sprintf("BAD_VERSION_%d", v)
 321  	}
 322  	return fmt.Sprintf("VERSION_%d", v)
 323  }
 324  
 325  func (v Variant) String() string {
 326  	switch v {
 327  	case RFC4122:
```

### uuid#631  version6.go:41  operator COR  reached by some test: yes
change: `err != nil`  →  `false`
```go
  33  // sequence, and a specified time. It is similar to the NewV6 function, but allows
  34  // you to specify the time. If time is passed as nil, then the current time is used.
  35  //
  36  // There is a limit on how many UUIDs can be generated for the same time, so if you
  37  // are generating multiple UUIDs, it is recommended to increment the time.
  38  // If getTime fails to return the current NewV6WithTime returns Nil and an error.
  39  func NewV6WithTime(customTime *time.Time) (UUID, error) {
  40  	now, seq, err := getTime(customTime)
  41> 	if err != nil {
  42  		return Nil, err
  43  	}
  44  
  45  	return generateV6(now, seq), nil
  46  }
  47  
  48  func generateV6(now Time, seq uint16) UUID {
  49  	var uuid UUID
```