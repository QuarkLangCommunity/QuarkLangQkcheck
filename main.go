// qkcheck: QuarkLang 静态检查（lint）——工具链成员
//
// 用法: qkcheck [-json] [-no-unused] [-no-unreachable] [-no-shadow] files...
//
// 检查项:
//
//	未使用局部变量  声明后从未被读取（params 不算；名字以 _ 开头跳过）
//	不可达代码      同一块中 return/break/log 之后还有语句
//	变量遮蔽        内层声明与外层（或形参）同名
//
// 退出码: 有警告 1；无警告 0；无法编译 2（此时打印编译错误）。
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"quarklang/internal/lang"
)

type warning struct {
	File string
	Line int
	Col  int
	Kind string
	Msg  string
}

type checker struct {
	file    string
	warns   []warning
	reads   map[string]int
	scopes  []map[string]bool
	params  map[string]bool
	unused  bool
	unreach bool
	shadow  bool
}

func (c *checker) warn(p lang.Pos, kind, msg string) {
	c.warns = append(c.warns, warning{c.file, p.Line, p.Col, kind, msg})
}

func (c *checker) push() { c.scopes = append(c.scopes, map[string]bool{}) }
func (c *checker) pop()  { c.scopes = c.scopes[:len(c.scopes)-1] }

func (c *checker) declare(name string, p lang.Pos) {
	if name == "" || strings.HasPrefix(name, "_") {
		return
	}
	if c.shadow {
		if c.params[name] {
			c.warn(p, "shadow", fmt.Sprintf("变量 %q 与形参同名（遮蔽）", name))
		} else {
			for i := 0; i < len(c.scopes)-1; i++ {
				if c.scopes[i][name] {
					c.warn(p, "shadow", fmt.Sprintf("变量 %q 遮蔽了外层同名变量", name))
					break
				}
			}
		}
	}
	c.scopes[len(c.scopes)-1][name] = true
}

// checkBlock 线性检查不可达代码，并递归处理子块。
func (c *checker) checkBlock(b *lang.Block) {
	if b == nil {
		return
	}
	c.push()
	terminated := false
	for _, st := range b.Stmts {
		if terminated && c.unreach {
			c.warn(stmtPos(st), "unreachable", "不可达代码：前一条语句已结束控制流")
			terminated = false // 只报一次，避免刷屏
		}
		c.checkStmt(st)
		switch st.(type) {
		case *lang.ReturnStmt, *lang.BreakStmt, *lang.LogStmt:
			terminated = true
		}
	}
	c.pop()
}

func stmtPos(st lang.Stmt) lang.Pos {
	switch s := st.(type) {
	case *lang.DeclStmt:
		return s.Pos
	case *lang.AssignStmt:
		return s.Pos
	case *lang.ReturnStmt:
		return s.Pos
	case *lang.BreakStmt:
		return s.Pos
	case *lang.LogStmt:
		return s.Pos
	case *lang.ExprStmt:
		return exprPos(s.X)
	case *lang.IfStmt:
		return exprPos(s.Cond)
	case *lang.WhileStmt:
		return exprPos(s.Cond)
	case *lang.ForStmt:
		return s.Pos
	case *lang.ForCStmt:
		return s.Pos
	case *lang.TryStmt:
		return s.Pos
	case *lang.DeleteStmt:
		return s.Pos
	}
	return lang.Pos{}
}

func (c *checker) checkStmt(st lang.Stmt) {
	switch s := st.(type) {
	case *lang.DeclStmt:
		c.checkExpr(s.Init)
		c.declare(s.Name, s.Pos)
	case *lang.AssignStmt:
		// 目标为变量时算写入；下标/成员目标算读取
		if id, ok := s.Target.(*lang.Ident); ok {
			c.reads[id.Name] += 0
		} else {
			c.checkExpr(s.Target)
		}
		c.checkExpr(s.X)
	case *lang.ExprStmt:
		c.checkExpr(s.X)
	case *lang.LogStmt:
		c.checkExpr(s.X)
	case *lang.DeleteStmt:
		c.checkExpr(s.X)
	case *lang.ReturnStmt:
		c.checkExpr(s.X)
	case *lang.IfStmt:
		c.checkExpr(s.Cond)
		c.checkBlock(s.Then)
		if s.Else != nil {
			c.checkBlock(s.Else)
		}
	case *lang.WhileStmt:
		c.checkExpr(s.Cond)
		c.checkBlock(s.Body)
	case *lang.ForStmt:
		c.checkExpr(s.Iter)
		c.push()
		c.declare(s.Var, s.Pos)
		c.checkBlock(s.Body)
		c.pop()
	case *lang.ForCStmt:
		c.push()
		c.checkStmt(s.Init)
		c.checkExpr(s.Cond)
		c.checkBlock(s.Body)
		if s.Step != nil {
			c.checkStmt(s.Step)
		}
		c.pop()
	case *lang.TryStmt:
		c.checkBlock(s.Try)
		c.push()
		c.declare(s.CatchVar, s.Pos)
		c.checkBlock(s.Catch)
		c.pop()
	}
}

func exprPos(e lang.Expr) lang.Pos {
	switch x := e.(type) {
	case *lang.Ident:
		return x.Pos
	case *lang.IntLit:
		return x.Pos
	case *lang.FloatLit:
		return x.Pos
	case *lang.StrLit:
		return x.Pos
	case *lang.BoolLit:
		return x.Pos
	case *lang.BinOp:
		return x.Pos
	case *lang.UnOp:
		return x.Pos
	case *lang.CallExpr:
		return x.Pos
	case *lang.MemberExpr:
		return x.Pos
	case *lang.IndexExpr:
		return x.Pos
	case *lang.StructLit:
		return x.Pos
	case *lang.ScopeCall:
		return x.Pos
	case *lang.NewExpr:
		return x.Pos
	}
	return lang.Pos{}
}

func (c *checker) checkExpr(e lang.Expr) {
	switch x := e.(type) {
	case nil:
		return
	case *lang.Ident:
		c.reads[x.Name]++
	case *lang.ListLit:
		for _, it := range x.Items {
			c.checkExpr(it)
		}
	case *lang.StructLit:
		for _, f := range x.Fields {
			c.checkExpr(f.X)
		}
	case *lang.NewExpr:
		c.checkExpr(x.Size)
	case *lang.BinOp:
		c.checkExpr(x.L)
		c.checkExpr(x.R)
	case *lang.UnOp:
		c.checkExpr(x.X)
	case *lang.CallExpr:
		c.checkExpr(x.Fn)
		for _, a := range x.Args {
			c.checkExpr(a)
		}
		if x.Sign != nil {
			for _, a := range x.Sign.Args {
				c.checkExpr(a)
			}
		}
	case *lang.MemberExpr:
		c.checkExpr(x.X)
	case *lang.ScopeCall:
		for _, a := range x.Args {
			c.checkExpr(a)
		}
	case *lang.IndexExpr:
		c.checkExpr(x.X)
		c.checkExpr(x.Idx)
	}
}

// checkFunc 对单个函数做局部变量/遮蔽/不可达检查。
func (c *checker) checkFunc(f *lang.FuncDecl) {
	if f == nil || f.Body == nil {
		return
	}
	c.reads = map[string]int{}
	c.params = map[string]bool{}
	for _, p := range f.Params {
		c.params[p.Name] = true
	}
	c.scopes = nil
	c.push() // 函数最外层作用域
	// 形参计入最外层（避免遮蔽误报重复）
	for _, p := range f.Params {
		c.scopes[0][p.Name] = true
	}
	c.checkBlock(f.Body)
	// 未使用局部变量：收集本函数所有块里声明的名字与位置
	if c.unused {
		for name, pos := range c.declaredLocals(f.Body) {
			if c.params[name] || strings.HasPrefix(name, "_") {
				continue
			}
			if c.reads[name] == 0 {
				c.warn(pos, "unused", fmt.Sprintf("局部变量 %q 声明后未被使用", name))
			}
		}
	}
}

// declaredLocals 收集函数体内所有局部声明（名字 → 位置）。
func (c *checker) declaredLocals(b *lang.Block) map[string]lang.Pos {
	out := map[string]lang.Pos{}
	var walkBlock func(*lang.Block)
	walkBlock = func(bl *lang.Block) {
		if bl == nil {
			return
		}
		for _, st := range bl.Stmts {
			switch s := st.(type) {
			case *lang.DeclStmt:
				if _, dup := out[s.Name]; !dup {
					out[s.Name] = s.Pos
				}
			case *lang.IfStmt:
				walkBlock(s.Then)
				walkBlock(s.Else)
			case *lang.WhileStmt:
				walkBlock(s.Body)
			case *lang.ForStmt:
				if _, dup := out[s.Var]; !dup {
					out[s.Var] = s.Pos
				}
				walkBlock(s.Body)
			case *lang.ForCStmt:
				if d, ok := s.Init.(*lang.DeclStmt); ok {
					if _, dup := out[d.Name]; !dup {
						out[d.Name] = d.Pos
					}
				}
				walkBlock(s.Body)
			case *lang.TryStmt:
				walkBlock(s.Try)
				if s.CatchVar != "" {
					if _, dup := out[s.CatchVar]; !dup {
						out[s.CatchVar] = s.Pos
					}
				}
				walkBlock(s.Catch)
			}
		}
	}
	walkBlock(b)
	return out
}

func main() {
	args := os.Args[1:]
	jsonOut := false
	c := &checker{unused: true, unreach: true} // 语言本身禁止遮蔽（重复声明直接报错），故 -shadow 默认关闭
	var files []string
	for _, a := range args {
		switch a {
		case "-json":
			jsonOut = true
		case "-no-unused":
			c.unused = false
		case "-no-unreachable":
			c.unreach = false
		case "-shadow":
			c.shadow = true
		case "-h", "--help":
			fmt.Println("usage: qkcheck [-json] [-no-unused] [-no-unreachable] [-shadow] files...")
			return
		default:
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: qkcheck [-json] [-no-unused] [-no-unreachable] [-shadow] files...")
		os.Exit(2)
	}
	sort.Strings(files)
	total := 0
	compileErrs := 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "qkcheck:", err)
			os.Exit(2)
		}
		prog, err := lang.CompileWithImports(string(src), f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			compileErrs++
			continue
		}
		c.file = f
		c.warns = nil
		funcs := append([]*lang.FuncDecl{}, prog.Funcs...)
		for _, im := range prog.Impls {
			funcs = append(funcs, im.Methods...)
		}
		for _, fn := range funcs {
			c.checkFunc(fn)
		}
		total += len(c.warns)
		for _, w := range c.warns {
			if jsonOut {
				fmt.Printf("{\"file\":%q,\"line\":%d,\"col\":%d,\"kind\":%q,\"msg\":%q}\n", w.File, w.Line, w.Col, w.Kind, w.Msg)
			} else {
				fmt.Printf("%s:%d:%d: 警告: [%s] %s\n", w.File, w.Line, w.Col, w.Kind, w.Msg)
			}
		}
	}
	if !jsonOut {
		fmt.Printf("qkcheck: %d 个警告（%d 个文件）\n", total, len(files))
	}
	if total > 0 {
		os.Exit(1)
	}
	if compileErrs > 0 {
		os.Exit(2)
	}
}
