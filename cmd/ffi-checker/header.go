package main

import (
	"fmt"
	"strconv"
	"strings"
)

// CAPI is the complete catalog read from the configured public headers.
// Every collection is keyed by the C identifier used by downstream bindings.
type CAPI struct {
	Functions map[string]CFunction
	Structs   map[string]CStruct
	Enums     map[string]CEnum
	Constants map[string]CConstant
	Callbacks map[string]CCallback
	Typedefs  map[string]CTypedef
	Issues    []CParseIssue

	ExportedDeclarations int
}

type CType struct {
	Raw  string
	Kind CKind
	Size int
}

type CKind int

const (
	CUnknown CKind = iota
	CVoid
	CSigned
	CUnsigned
	CFloat
	CDouble
	CPointer
	CStructValue
)

func (k CKind) String() string {
	switch k {
	case CVoid:
		return "void"
	case CSigned:
		return "signed"
	case CUnsigned:
		return "unsigned"
	case CFloat:
		return "float"
	case CDouble:
		return "double"
	case CPointer:
		return "pointer"
	case CStructValue:
		return "struct"
	default:
		return "unknown"
	}
}

type CParseIssue struct {
	Kind, Name, Detail, File string
	Line                     int
}

type CParameter struct {
	Name string
	Type CType
	Raw  string
}

type CFunction struct {
	Name       string
	ReturnType CType
	Parameters []CParameter
	Variadic   bool
	File       string
	Line       int
	Deprecated bool
}

type CField struct {
	Name       string
	Type       CType
	ArrayCount string
	Count      int
	Offset     int
	Raw        string
}

type CStruct struct {
	Name        string
	Fields      []CField
	Union       bool
	Size        int
	Align       int
	LayoutError string
	File        string
	Line        int
}

type CEnumMember struct {
	Name     string
	Expr     string
	Value    int64
	Resolved bool
	File     string
	Line     int
}

type CEnum struct {
	Name    string
	Members []CEnumMember
	File    string
	Line    int
}

type CConstant struct {
	Name     string
	Expr     string
	Value    int64
	Resolved bool
	Enum     string
	File     string
	Line     int
	Reason   string
}

type CCallback struct {
	Name       string
	ReturnType CType
	Parameters []CParameter
	Variadic   bool
	File       string
	Line       int
}

type CTypedef struct {
	Name string
	Type CType
	File string
	Line int
}

func (api *CAPI) ResolvedConstants() int {
	n := 0
	for _, c := range api.Constants {
		if c.Resolved {
			n++
		}
	}
	return n
}

func (api *CAPI) UnresolvedConstants() int {
	return len(api.Constants) - api.ResolvedConstants()
}

func (api *CAPI) ResolvedLayouts() int {
	n := 0
	for _, st := range api.Structs {
		if st.LayoutError == "" {
			n++
		}
	}
	return n
}

// token is one lexical C token. Comments are discarded by scanC, while byte
// offsets and lines remain tied to the original source for useful locations.
type tokenC struct {
	text       string
	start, end int
	line       int
}

type parsedHeader struct {
	header Header
	tokens []tokenC
}

type rawConstant struct {
	name, expr, enum string
	file             string
	line             int
	implicit         bool
	previous         string
}

func ParseCAPI(headers []Header) (*CAPI, error) {
	api := &CAPI{
		Functions: map[string]CFunction{},
		Structs:   map[string]CStruct{},
		Enums:     map[string]CEnum{},
		Constants: map[string]CConstant{},
		Callbacks: map[string]CCallback{},
		Typedefs:  map[string]CTypedef{},
	}

	var constants []rawConstant
	for _, h := range headers {
		tokens, err := scanC(h.Source)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", h.Name, err)
		}
		ph := parsedHeader{header: h, tokens: tokens}

		if err := collectFunctions(api, ph); err != nil {
			return nil, err
		}
		if err := collectAggregates(api, ph, &constants); err != nil {
			return nil, err
		}
		if err := collectTypedefs(api, ph, &constants); err != nil {
			return nil, err
		}
		constants = append(constants, collectDefines(ph)...)
	}

	resolveConstants(api, constants)
	for name, enum := range api.Enums {
		for i := range enum.Members {
			if c, ok := api.Constants[enum.Members[i].Name]; ok {
				enum.Members[i].Value = c.Value
				enum.Members[i].Resolved = c.Resolved
			}
		}
		api.Enums[name] = enum
	}
	resolveTypes(api)

	return api, nil
}

func scanC(src string) ([]tokenC, error) {
	var out []tokenC
	line := 1
	for i := 0; i < len(src); {
		switch {
		case isSpace(src[i]):
			if src[i] == '\n' {
				line++
			}
			i++
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			start := i
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			if i+1 >= len(src) {
				return nil, fmt.Errorf("line %d: unterminated comment at byte %d", line, start)
			}
			i += 2
		case isIdentStart(src[i]):
			start := i
			i++
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			out = append(out, tokenC{text: src[start:i], start: start, end: i, line: line})
		case src[i] >= '0' && src[i] <= '9':
			start := i
			i++
			for i < len(src) && (isIdentPart(src[i]) || src[i] == '.') {
				i++
			}
			out = append(out, tokenC{text: src[start:i], start: start, end: i, line: line})
		case src[i] == '"' || src[i] == '\'':
			quote, start := src[i], i
			i++
			for i < len(src) && src[i] != quote {
				if src[i] == '\\' && i+1 < len(src) {
					i += 2
					continue
				}
				if src[i] == '\n' {
					line++
				}
				i++
			}
			if i >= len(src) {
				return nil, fmt.Errorf("line %d: unterminated literal", line)
			}
			i++
			out = append(out, tokenC{text: src[start:i], start: start, end: i, line: line})
		default:
			start := i
			text := src[i : i+1]
			if i+1 < len(src) {
				two := src[i : i+2]
				if two == "<<" || two == ">>" || two == "::" || two == "->" || two == "&&" || two == "||" {
					text = two
					i++
				}
			}
			i++
			out = append(out, tokenC{text: text, start: start, end: i, line: line})
		}
	}
	return out, nil
}

func collectFunctions(api *CAPI, ph parsedHeader) error {
	macros := map[string]bool{}
	for _, macro := range ph.header.APIMacros {
		macros[macro] = true
	}
	for i, tok := range ph.tokens {
		if !macros[tok.text] || onDirectiveLine(ph.tokens, i) {
			continue
		}
		api.ExportedDeclarations++
		end := statementEnd(ph.tokens, i+1)
		if end < 0 {
			api.Issues = append(api.Issues, CParseIssue{Kind: "function", File: ph.header.Name, Line: tok.line, Detail: "exported declaration has no terminating semicolon"})
			continue
		}
		fn, ok := parseFunction(ph.tokens[i+1:end], ph.header.Name, tok.line)
		if !ok {
			api.Issues = append(api.Issues, CParseIssue{Kind: "function", File: ph.header.Name, Line: tok.line, Detail: "cannot parse " + tokenText(ph.tokens[i+1:end])})
			continue
		}
		fn.Deprecated = insideDeprecated(ph.tokens, i)
		if old, exists := api.Functions[fn.Name]; exists && functionSignature(old) != functionSignature(fn) {
			return fmt.Errorf("%s:%d: conflicting declarations for %s", ph.header.Name, tok.line, fn.Name)
		}
		api.Functions[fn.Name] = fn
	}
	return nil
}

func parseFunction(ts []tokenC, file string, line int) (CFunction, bool) {
	open := firstTop(ts, "(")
	if open <= 0 || !isIdentifier(ts[open-1].text) {
		return CFunction{}, false
	}
	close := matching(ts, open, "(", ")")
	if close < 0 {
		return CFunction{}, false
	}
	fn := CFunction{
		Name:       ts[open-1].text,
		ReturnType: CType{Raw: typeText(ts[:open-1])},
		File:       file,
		Line:       line,
	}
	if fn.ReturnType.Raw == "" {
		return CFunction{}, false
	}
	fn.Parameters, fn.Variadic = parseParameters(ts[open+1 : close])
	return fn, true
}

func parseParameters(ts []tokenC) ([]CParameter, bool) {
	if len(ts) == 0 || len(ts) == 1 && ts[0].text == "void" {
		return nil, false
	}
	var params []CParameter
	variadic := false
	for _, part := range splitTokens(ts, ",") {
		if len(part) == 1 && part[0].text == "..." || len(part) == 3 && part[0].text == "." && part[1].text == "." && part[2].text == "." {
			variadic = true
			continue
		}
		if len(part) == 0 {
			continue
		}
		params = append(params, parseParameter(part))
	}
	return params, variadic
}

func parseParameter(ts []tokenC) CParameter {
	raw := typeText(ts)
	if name, _, ok := functionPointerName(ts); ok {
		return CParameter{Name: name, Type: CType{Raw: "function pointer"}, Raw: raw}
	}
	nameAt := declaratorNameIndex(ts)
	if nameAt < 0 {
		return CParameter{Type: CType{Raw: raw}, Raw: raw}
	}
	typeTokens := append([]tokenC(nil), ts[:nameAt]...)
	if nameAt+1 < len(ts) && ts[nameAt+1].text == "[" {
		typeTokens = append(typeTokens, tokenC{text: "*"})
	}
	return CParameter{Name: ts[nameAt].text, Type: CType{Raw: typeText(typeTokens)}, Raw: raw}
}

func collectAggregates(api *CAPI, ph parsedHeader, constants *[]rawConstant) error {
	for i := 0; i+2 < len(ph.tokens); i++ {
		switch ph.tokens[i].text {
		case "struct", "union":
			if !isIdentifier(ph.tokens[i+1].text) || ph.tokens[i+2].text != "{" {
				continue
			}
			kind := ph.tokens[i].text
			close := matching(ph.tokens, i+2, "{", "}")
			if close < 0 {
				return fmt.Errorf("%s:%d: unterminated %s %s", ph.header.Name, ph.tokens[i].line, kind, ph.tokens[i+1].text)
			}
			body := ph.tokens[i+3 : close]
			layoutError := unsupportedAggregate(body)
			if kind == "union" {
				layoutError = "union layout is unsupported"
			}
			st := CStruct{Name: ph.tokens[i+1].text, Union: kind == "union", Size: -1, Align: -1, LayoutError: layoutError, File: ph.header.Name, Line: ph.tokens[i].line}
			for _, stmt := range splitTokens(body, ";") {
				st.Fields = append(st.Fields, parseFields(stmt)...)
			}
			if old, exists := api.Structs[st.Name]; exists && (old.Union != st.Union || len(old.Fields) != len(st.Fields)) {
				return fmt.Errorf("%s:%d: conflicting definitions for aggregate %s", ph.header.Name, st.Line, st.Name)
			}
			api.Structs[st.Name] = st
			i = close
		case "enum":
			if !isIdentifier(ph.tokens[i+1].text) || ph.tokens[i+2].text != "{" {
				continue
			}
			close := matching(ph.tokens, i+2, "{", "}")
			if close < 0 {
				return fmt.Errorf("%s:%d: unterminated enum %s", ph.header.Name, ph.tokens[i].line, ph.tokens[i+1].text)
			}
			en := CEnum{Name: ph.tokens[i+1].text, File: ph.header.Name, Line: ph.tokens[i].line}
			previous := ""
			for _, member := range splitTokens(ph.tokens[i+3:close], ",") {
				if len(member) == 0 || !isIdentifier(member[0].text) {
					continue
				}
				m := CEnumMember{Name: member[0].text, File: ph.header.Name, Line: member[0].line}
				raw := rawConstant{name: m.Name, enum: en.Name, file: m.File, line: m.Line, previous: previous}
				if len(member) > 1 && member[1].text == "=" {
					m.Expr = expressionText(member[2:])
					raw.expr = m.Expr
				} else {
					raw.implicit = true
				}
				en.Members = append(en.Members, m)
				*constants = append(*constants, raw)
				previous = m.Name
			}
			api.Enums[en.Name] = en
			i = close
		}
	}
	return nil
}

func parseFields(ts []tokenC) []CField {
	if len(ts) == 0 {
		return nil
	}
	if len(ts) > 3 && ts[0].text == "struct" && ts[1].text == "{" {
		close := matching(ts, 1, "{", "}")
		if close > 1 {
			prefix := ""
			if close+1 < len(ts) && isIdentifier(ts[close+1].text) {
				prefix = ts[close+1].text
			}
			var fields []CField
			for _, stmt := range splitTokens(ts[2:close], ";") {
				for _, field := range parseFields(stmt) {
					field.Name = joinMemberPath(prefix, field.Name)
					fields = append(fields, field)
				}
			}
			return fields
		}
	}
	raw := typeText(ts)
	if name, _, ok := functionPointerName(ts); ok {
		return []CField{{Name: name, Type: CType{Raw: "function pointer"}, Count: 1, Offset: -1, Raw: raw}}
	}
	parts := splitTokens(ts, ",")
	baseEnd := declaratorNameIndex(parts[0])
	if baseEnd < 0 {
		return nil
	}
	base := append([]tokenC(nil), parts[0][:baseEnd]...)
	var fields []CField
	for i, part := range parts {
		decl := part
		if i > 0 {
			decl = append(append([]tokenC(nil), base...), part...)
		}
		nameAt := declaratorNameIndex(decl)
		if nameAt < 0 {
			continue
		}
		field := CField{Name: decl[nameAt].text, Type: CType{Raw: typeText(decl[:nameAt])}, Count: 1, Offset: -1, Raw: typeText(decl)}
		if nameAt+1 < len(decl) && decl[nameAt+1].text == "[" {
			close := matching(decl, nameAt+1, "[", "]")
			if close > nameAt+1 {
				field.ArrayCount = expressionText(decl[nameAt+2 : close])
			}
		}
		fields = append(fields, field)
	}
	return fields
}

func unsupportedAggregate(body []tokenC) string {
	for i, tok := range body {
		switch tok.text {
		case "union":
			if i+1 < len(body) && (body[i+1].text == "{" || i+2 < len(body) && body[i+2].text == "{") {
				return "contains a nested union"
			}
		case ":":
			return "contains a bitfield"
		}
	}
	return ""
}

func collectTypedefs(api *CAPI, ph parsedHeader, constants *[]rawConstant) error {
	for i, tok := range ph.tokens {
		if tok.text != "typedef" || onDirectiveLine(ph.tokens, i) {
			continue
		}
		end := statementEnd(ph.tokens, i+1)
		if end < 0 {
			return fmt.Errorf("%s:%d: typedef has no terminating semicolon", ph.header.Name, tok.line)
		}
		decl := ph.tokens[i+1 : end]
		if name, star, ok := functionPointerName(decl); ok {
			closeDecl := matching(decl, star-1, "(", ")")
			if closeDecl < 0 || closeDecl+1 >= len(decl) || decl[closeDecl+1].text != "(" {
				api.Issues = append(api.Issues, CParseIssue{Kind: "callback", Name: name, File: ph.header.Name, Line: tok.line, Detail: "no parameter list after function-pointer declarator"})
				continue
			}
			closeParams := matching(decl, closeDecl+1, "(", ")")
			if closeParams < 0 {
				api.Issues = append(api.Issues, CParseIssue{Kind: "callback", Name: name, File: ph.header.Name, Line: tok.line, Detail: "unterminated parameter list"})
				continue
			}
			params, variadic := parseParameters(decl[closeDecl+2 : closeParams])
			api.Callbacks[name] = CCallback{
				Name: name, ReturnType: CType{Raw: typeText(decl[:star-1])}, Parameters: params,
				Variadic: variadic, File: ph.header.Name, Line: tok.line,
			}
			api.Typedefs[name] = CTypedef{Name: name, Type: CType{Raw: "function pointer"}, File: ph.header.Name, Line: tok.line}
			i = end
			continue
		}

		nameAt := lastIdentifier(decl)
		if nameAt < 0 {
			continue
		}
		name := decl[nameAt].text
		under := decl[:nameAt]
		if len(under) >= 3 && (under[0].text == "struct" || under[0].text == "union") && under[1].text == "{" {
			kind := under[0].text
			close := matching(under, 1, "{", "}")
			if close < 0 {
				api.Issues = append(api.Issues, CParseIssue{Kind: kind, Name: name, File: ph.header.Name, Line: tok.line, Detail: "unterminated anonymous " + kind + " typedef"})
				continue
			}
			body := under[2:close]
			layoutError := unsupportedAggregate(body)
			if kind == "union" {
				layoutError = "union layout is unsupported"
			}
			st := CStruct{Name: name, Union: kind == "union", Size: -1, Align: -1, LayoutError: layoutError, File: ph.header.Name, Line: tok.line}
			for _, stmt := range splitTokens(body, ";") {
				st.Fields = append(st.Fields, parseFields(stmt)...)
			}
			api.Structs[name] = st
			under = []tokenC{{text: kind}, {text: name}}
		} else if len(under) >= 3 && under[0].text == "enum" && under[1].text == "{" {
			close := matching(under, 1, "{", "}")
			if close < 0 {
				api.Issues = append(api.Issues, CParseIssue{Kind: "enum", Name: name, File: ph.header.Name, Line: tok.line, Detail: "unterminated anonymous enum typedef"})
				continue
			}
			en, raw := parseEnumMembers(name, ph.header.Name, tok.line, under[2:close])
			api.Enums[name] = en
			*constants = append(*constants, raw...)
			under = []tokenC{{text: "enum"}, {text: name}}
		}
		if brace := lastToken(under, "}"); brace >= 0 {
			open := reverseMatching(under, brace, "{", "}")
			if open >= 2 && (under[open-2].text == "struct" || under[open-2].text == "union" || under[open-2].text == "enum") {
				under = under[open-2 : open]
			}
		}
		api.Typedefs[name] = CTypedef{Name: name, Type: CType{Raw: typeText(under)}, File: ph.header.Name, Line: tok.line}
		i = end
	}
	return nil
}

func parseEnumMembers(name, file string, line int, body []tokenC) (CEnum, []rawConstant) {
	en := CEnum{Name: name, File: file, Line: line}
	var constants []rawConstant
	previous := ""
	for _, member := range splitTokens(body, ",") {
		if len(member) == 0 || !isIdentifier(member[0].text) {
			continue
		}
		m := CEnumMember{Name: member[0].text, File: file, Line: member[0].line}
		raw := rawConstant{name: m.Name, enum: name, file: file, line: m.Line, previous: previous}
		if len(member) > 1 && member[1].text == "=" {
			m.Expr = expressionText(member[2:])
			raw.expr = m.Expr
		} else {
			raw.implicit = true
		}
		en.Members = append(en.Members, m)
		constants = append(constants, raw)
		previous = m.Name
	}
	return en, constants
}

func collectDefines(ph parsedHeader) []rawConstant {
	var out []rawConstant
	ts := ph.tokens
	for i := 0; i+2 < len(ts); i++ {
		if ts[i].text != "#" || ts[i+1].text != "define" || ts[i].line != ts[i+1].line || !isIdentifier(ts[i+2].text) {
			continue
		}
		name := ts[i+2]
		end := i + 3
		for end < len(ts) && ts[end].line == name.line {
			end++
		}
		if i+3 >= end || ts[i+3].text == "(" && ts[i+3].start == name.end {
			continue
		}
		out = append(out, rawConstant{name: name.text, expr: expressionText(ts[i+3 : end]), file: ph.header.Name, line: name.line})
		i = end - 1
	}
	return out
}

func resolveConstants(api *CAPI, raw []rawConstant) {
	type result struct {
		value  int64
		known  bool
		reason string
	}
	results := make([]result, len(raw))
	values := map[string]int64{}

	for range len(raw) + 1 {
		changed := false
		for i, r := range raw {
			if results[i].known {
				continue
			}
			var value int64
			var err error
			switch {
			case r.implicit && r.previous == "":
				value = 0
			case r.implicit:
				var ok bool
				value, ok = values[r.previous]
				if !ok {
					results[i].reason = "previous enum member " + r.previous + " is unresolved"
					continue
				}
				value++
			default:
				value, err = evalCExpression(r.expr, values)
				if err != nil {
					results[i].reason = err.Error()
					continue
				}
			}
			results[i] = result{value: value, known: true}
			changed = true
		}

		groups := map[string][]int{}
		for i, r := range raw {
			groups[r.name] = append(groups[r.name], i)
		}
		for name, indexes := range groups {
			first, all := int64(0), true
			for j, idx := range indexes {
				if !results[idx].known {
					all = false
					break
				}
				if j == 0 {
					first = results[idx].value
				} else if results[idx].value != first {
					all = false
					break
				}
			}
			if all {
				values[name] = first
			} else {
				delete(values, name)
			}
		}
		if !changed {
			break
		}
	}

	groups := map[string][]int{}
	for i, r := range raw {
		groups[r.name] = append(groups[r.name], i)
	}
	for name, indexes := range groups {
		r := raw[indexes[0]]
		c := CConstant{Name: name, Expr: r.expr, Enum: r.enum, File: r.file, Line: r.line}
		var first int64
		allKnown, same, haveFirst := true, true, false
		var reasons []string
		for _, idx := range indexes {
			if !results[idx].known {
				allKnown = false
				if results[idx].reason != "" {
					reasons = append(reasons, results[idx].reason)
				}
				continue
			}
			if !haveFirst {
				first = results[idx].value
				haveFirst = true
			} else if results[idx].value != first {
				same = false
			}
		}
		switch {
		case allKnown && same:
			c.Value, c.Resolved = first, true
		case !same:
			c.Reason = "multiple conditional definitions have different values"
		case len(reasons) > 0:
			c.Reason = reasons[0]
		default:
			c.Reason = "expression could not be evaluated"
		}
		api.Constants[name] = c
	}
}

var scalarTypes = map[string]CType{
	"void":               {Kind: CVoid, Size: 0},
	"bool":               {Kind: CUnsigned, Size: 1},
	"_Bool":              {Kind: CUnsigned, Size: 1},
	"char":               {Kind: CSigned, Size: 1},
	"signed char":        {Kind: CSigned, Size: 1},
	"unsigned char":      {Kind: CUnsigned, Size: 1},
	"short":              {Kind: CSigned, Size: 2},
	"unsigned short":     {Kind: CUnsigned, Size: 2},
	"int":                {Kind: CSigned, Size: 4},
	"unsigned":           {Kind: CUnsigned, Size: 4},
	"unsigned int":       {Kind: CUnsigned, Size: 4},
	"long":               {Kind: CSigned, Size: 8},
	"unsigned long":      {Kind: CUnsigned, Size: 8},
	"long long":          {Kind: CSigned, Size: 8},
	"unsigned long long": {Kind: CUnsigned, Size: 8},
	"float":              {Kind: CFloat, Size: 4},
	"double":             {Kind: CDouble, Size: 8},
	"int8_t":             {Kind: CSigned, Size: 1},
	"uint8_t":            {Kind: CUnsigned, Size: 1},
	"int16_t":            {Kind: CSigned, Size: 2},
	"uint16_t":           {Kind: CUnsigned, Size: 2},
	"int32_t":            {Kind: CSigned, Size: 4},
	"uint32_t":           {Kind: CUnsigned, Size: 4},
	"int64_t":            {Kind: CSigned, Size: 8},
	"uint64_t":           {Kind: CUnsigned, Size: 8},
	"size_t":             {Kind: CUnsigned, Size: 8},
	"ssize_t":            {Kind: CSigned, Size: 8},
	"ptrdiff_t":          {Kind: CSigned, Size: 8},
	"intptr_t":           {Kind: CSigned, Size: 8},
	"uintptr_t":          {Kind: CUnsigned, Size: 8},
}

func resolveTypes(api *CAPI) {
	blocked := map[string]string{}
	for name, st := range api.Structs {
		if st.LayoutError != "" {
			blocked[name] = st.LayoutError
		}
	}

	for range 16 {
		for name, st := range api.Structs {
			if why := blocked[name]; why != "" {
				st.Size, st.Align, st.LayoutError = -1, -1, why
				api.Structs[name] = st
				continue
			}
			layoutStruct(api, &st)
			api.Structs[name] = st
		}
	}

	for name, td := range api.Typedefs {
		td.Type = classifyType(api, td.Type.Raw, map[string]bool{})
		api.Typedefs[name] = td
	}
	for name, fn := range api.Functions {
		fn.ReturnType = classifyType(api, fn.ReturnType.Raw, map[string]bool{})
		for i := range fn.Parameters {
			fn.Parameters[i].Type = classifyType(api, fn.Parameters[i].Type.Raw, map[string]bool{})
		}
		api.Functions[name] = fn
	}
	for name, cb := range api.Callbacks {
		cb.ReturnType = classifyType(api, cb.ReturnType.Raw, map[string]bool{})
		for i := range cb.Parameters {
			cb.Parameters[i].Type = classifyType(api, cb.Parameters[i].Type.Raw, map[string]bool{})
		}
		api.Callbacks[name] = cb
	}
}

func layoutStruct(api *CAPI, st *CStruct) {
	offset, align := 0, 1
	st.LayoutError = ""
	for i := range st.Fields {
		field := &st.Fields[i]
		field.Type = classifyType(api, field.Type.Raw, map[string]bool{})
		field.Count = 1
		if field.ArrayCount != "" {
			values := map[string]int64{}
			for name, c := range api.Constants {
				if c.Resolved {
					values[name] = c.Value
				}
			}
			count, err := evalCExpression(field.ArrayCount, values)
			if err != nil || count <= 0 || count > 1<<20 {
				field.Count = -1
				st.Size, st.Align, st.LayoutError = -1, -1, "field "+field.Name+" has an unresolved array count"
				return
			}
			field.Count = int(count)
		}
		if field.Type.Kind == CUnknown || field.Type.Size <= 0 || field.Count <= 0 {
			st.Size, st.Align, st.LayoutError = -1, -1, "field "+field.Name+" has unresolved type "+field.Type.Raw
			return
		}
		fieldAlign := min(field.Type.Size, 8)
		if field.Type.Kind == CStructValue {
			name := structTypeName(api, field.Type.Raw)
			if nested, ok := api.Structs[name]; ok && nested.Align > 0 {
				fieldAlign = nested.Align
			}
		}
		if offset%fieldAlign != 0 {
			offset += fieldAlign - offset%fieldAlign
		}
		field.Offset = offset
		offset += field.Type.Size * field.Count
		align = max(align, fieldAlign)
	}
	if offset%align != 0 {
		offset += align - offset%align
	}
	st.Size, st.Align = offset, align
}

func classifyType(api *CAPI, raw string, seen map[string]bool) CType {
	t := normalizeType(raw)
	result := CType{Raw: raw, Kind: CUnknown, Size: -1}
	if t == "function pointer" || strings.Contains(t, "*") || strings.Contains(t, "[") {
		result.Kind, result.Size = CPointer, 8
		return result
	}
	if scalar, ok := scalarTypes[t]; ok {
		result.Kind, result.Size = scalar.Kind, scalar.Size
		return result
	}
	if strings.HasPrefix(t, "enum ") {
		result.Kind, result.Size = CSigned, 4
		return result
	}
	if strings.HasPrefix(t, "struct ") {
		name := strings.TrimSpace(strings.TrimPrefix(t, "struct "))
		result.Kind = CStructValue
		if st, ok := api.Structs[name]; ok {
			result.Size = st.Size
		}
		return result
	}
	if strings.HasPrefix(t, "union ") {
		result.Kind = CStructValue
		return result
	}
	if _, ok := api.Callbacks[t]; ok {
		result.Kind, result.Size = CPointer, 8
		return result
	}
	if seen[t] {
		return result
	}
	if td, ok := api.Typedefs[t]; ok {
		seen[t] = true
		resolved := classifyType(api, td.Type.Raw, seen)
		resolved.Raw = raw
		return resolved
	}
	return result
}

func normalizeType(raw string) string {
	parts := strings.Fields(raw)
	out := parts[:0]
	for _, part := range parts {
		switch part {
		case "const", "volatile", "restrict", "GGML_RESTRICT":
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, " ")
}

func structTypeName(api *CAPI, raw string) string {
	t := normalizeType(raw)
	for range 12 {
		if name, ok := strings.CutPrefix(t, "struct "); ok {
			return strings.TrimSpace(name)
		}
		td, ok := api.Typedefs[t]
		if !ok {
			return ""
		}
		t = normalizeType(td.Type.Raw)
	}
	return ""
}

func insideDeprecated(ts []tokenC, at int) bool {
	depth := 0
	for i := at - 1; i >= 0 && at-i < 64; i-- {
		switch ts[i].text {
		case ")":
			depth++
		case "(":
			if depth > 0 {
				depth--
				continue
			}
			if i > 0 && strings.HasSuffix(ts[i-1].text, "_DEPRECATED") {
				return true
			}
		}
	}
	return false
}

func onDirectiveLine(ts []tokenC, at int) bool {
	line := ts[at].line
	for i := at - 1; i >= 0 && ts[i].line == line; i-- {
		if ts[i].text == "#" {
			return true
		}
	}
	return false
}

func statementEnd(ts []tokenC, start int) int {
	paren, brace, bracket := 0, 0, 0
	for i := start; i < len(ts); i++ {
		switch ts[i].text {
		case "(":
			paren++
		case ")":
			paren--
		case "{":
			brace++
		case "}":
			brace--
		case "[":
			bracket++
		case "]":
			bracket--
		case ";":
			if paren <= 0 && brace == 0 && bracket == 0 {
				return i
			}
		}
	}
	return -1
}

func splitTokens(ts []tokenC, delimiter string) [][]tokenC {
	var out [][]tokenC
	start, paren, brace, bracket := 0, 0, 0, 0
	for i, tok := range ts {
		switch tok.text {
		case "(":
			paren++
		case ")":
			paren--
		case "{":
			brace++
		case "}":
			brace--
		case "[":
			bracket++
		case "]":
			bracket--
		default:
			if tok.text == delimiter && paren == 0 && brace == 0 && bracket == 0 {
				out = append(out, ts[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, ts[start:])
	return out
}

func firstTop(ts []tokenC, text string) int {
	paren, brace, bracket := 0, 0, 0
	for i, tok := range ts {
		if tok.text == text && paren == 0 && brace == 0 && bracket == 0 {
			return i
		}
		switch tok.text {
		case "(":
			paren++
		case ")":
			paren--
		case "{":
			brace++
		case "}":
			brace--
		case "[":
			bracket++
		case "]":
			bracket--
		}
	}
	return -1
}

func matching(ts []tokenC, open int, left, right string) int {
	depth := 0
	for i := open; i < len(ts); i++ {
		if ts[i].text == left {
			depth++
		} else if ts[i].text == right {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func reverseMatching(ts []tokenC, close int, left, right string) int {
	depth := 0
	for i := close; i >= 0; i-- {
		if ts[i].text == right {
			depth++
		} else if ts[i].text == left {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func functionPointerName(ts []tokenC) (name string, star int, ok bool) {
	for i := 1; i+2 < len(ts); i++ {
		if ts[i-1].text == "(" && ts[i].text == "*" && isIdentifier(ts[i+1].text) && ts[i+2].text == ")" {
			return ts[i+1].text, i, true
		}
	}
	return "", -1, false
}

func declaratorNameIndex(ts []tokenC) int {
	for i := len(ts) - 1; i >= 0; i-- {
		if !isIdentifier(ts[i].text) || isTypeKeyword(ts[i].text) {
			continue
		}
		if i > 0 && (ts[i-1].text == "struct" || ts[i-1].text == "enum" || ts[i-1].text == "union") {
			continue
		}
		return i
	}
	return -1
}

func lastIdentifier(ts []tokenC) int {
	for i := len(ts) - 1; i >= 0; i-- {
		if isIdentifier(ts[i].text) {
			return i
		}
	}
	return -1
}

func lastToken(ts []tokenC, text string) int {
	for i := len(ts) - 1; i >= 0; i-- {
		if ts[i].text == text {
			return i
		}
	}
	return -1
}

func typeText(ts []tokenC) string {
	if len(ts) == 0 {
		return ""
	}
	var b strings.Builder
	for i, tok := range ts {
		if i > 0 && needsSpace(ts[i-1].text, tok.text) {
			b.WriteByte(' ')
		}
		b.WriteString(tok.text)
	}
	return strings.TrimSpace(b.String())
}

func expressionText(ts []tokenC) string { return typeText(ts) }
func tokenText(ts []tokenC) string      { return typeText(ts) }

func needsSpace(left, right string) bool {
	if right == ")" || right == "]" || right == "," || right == ";" || left == "(" || left == "[" {
		return false
	}
	if right == "*" || left == "*" {
		return true
	}
	return isWord(left) && isWord(right)
}

func functionSignature(fn CFunction) string {
	var b strings.Builder
	b.WriteString(fn.ReturnType.Raw)
	b.WriteByte('(')
	for _, p := range fn.Parameters {
		b.WriteString(p.Type.Raw)
		b.WriteByte(',')
	}
	if fn.Variadic {
		b.WriteString("...")
	}
	return b.String()
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' }
func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
func isIdentPart(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }
func isIdentifier(s string) bool {
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentPart(s[i]) {
			return false
		}
	}
	return true
}
func isWord(s string) bool {
	return isIdentifier(s) || s != "" && s[0] >= '0' && s[0] <= '9' || strings.HasPrefix(s, "\"") || strings.HasPrefix(s, "'")
}

func isTypeKeyword(s string) bool {
	switch s {
	case "void", "bool", "char", "short", "int", "long", "float", "double", "signed", "unsigned", "const", "volatile", "restrict", "struct", "enum", "union":
		return true
	}
	return false
}

func evalCExpression(expr string, values map[string]int64) (int64, error) {
	ts, err := scanC(expr)
	if err != nil {
		return 0, err
	}
	texts := make([]string, len(ts))
	for i := range ts {
		texts[i] = ts[i].text
	}
	p := expressionParser{tokens: texts, values: values}
	value, err := p.parse(0)
	if err != nil {
		return 0, err
	}
	if p.at != len(p.tokens) {
		return 0, fmt.Errorf("unsupported token %q", p.tokens[p.at])
	}
	return value, nil
}

type expressionParser struct {
	tokens []string
	values map[string]int64
	at     int
}

var precedence = map[string]int{
	"|": 1, "^": 2, "&": 3,
	"<<": 4, ">>": 4,
	"+": 5, "-": 5,
	"*": 6, "/": 6, "%": 6,
}

func (p *expressionParser) parse(min int) (int64, error) {
	left, err := p.unary()
	if err != nil {
		return 0, err
	}
	for p.at < len(p.tokens) {
		prec, ok := precedence[p.tokens[p.at]]
		if !ok || prec < min {
			break
		}
		op := p.tokens[p.at]
		p.at++
		right, err := p.parse(prec + 1)
		if err != nil {
			return 0, err
		}
		switch op {
		case "|":
			left |= right
		case "^":
			left ^= right
		case "&":
			left &= right
		case "<<":
			if right < 0 || right > 63 {
				return 0, fmt.Errorf("invalid shift")
			}
			left <<= uint(right)
		case ">>":
			if right < 0 || right > 63 {
				return 0, fmt.Errorf("invalid shift")
			}
			left >>= uint(right)
		case "+":
			left += right
		case "-":
			left -= right
		case "*":
			left *= right
		case "/":
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			left /= right
		case "%":
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			left %= right
		}
	}
	return left, nil
}

func (p *expressionParser) unary() (int64, error) {
	if p.at >= len(p.tokens) {
		return 0, fmt.Errorf("empty expression")
	}
	switch p.tokens[p.at] {
	case "+":
		p.at++
		return p.unary()
	case "-":
		p.at++
		v, err := p.unary()
		return -v, err
	case "~":
		p.at++
		v, err := p.unary()
		return ^v, err
	case "(":
		p.at++
		v, err := p.parse(0)
		if err != nil {
			return 0, err
		}
		if p.at >= len(p.tokens) || p.tokens[p.at] != ")" {
			return 0, fmt.Errorf("unclosed parenthesis")
		}
		p.at++
		return v, nil
	}
	tok := p.tokens[p.at]
	p.at++
	if isIdentifier(tok) {
		v, ok := p.values[tok]
		if !ok {
			return 0, fmt.Errorf("unknown identifier %s", tok)
		}
		return v, nil
	}
	literal := strings.TrimRight(tok, "uUlL")
	if v, err := strconv.ParseInt(literal, 0, 64); err == nil {
		return v, nil
	}
	if v, err := strconv.ParseUint(literal, 0, 64); err == nil {
		return int64(v), nil
	}
	return 0, fmt.Errorf("unsupported literal %s", tok)
}
