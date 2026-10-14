package dbinit

import "strings"

// splitStatements 按分号切分 SQL 文本为可执行语句序列：
//   - 单引号 '…'、双引号 "…"、反引号 `…` 字面量内的分号不算语句边界；
//   - MySQL 反斜杠转义（'\”）与相邻双写引号（'it”s'）均在字面量内原样保留；
//   - -- 行注释与 /* */ 块注释整体剥离（含被注释掉的分号）；
//   - 纯注释/空白段不产出语句。
//
// 方言边界：本包面向 MySQL（生产）与 sqlite（单测），不处理 PG 的
// dollar-quoted 字符串——当前脚本不含该语法。
func splitStatements(sql string) []string {
	var stmts []string
	var b strings.Builder
	flush := func() {
		if s := strings.TrimSpace(b.String()); s != "" {
			stmts = append(stmts, s)
		}
		b.Reset()
	}
	i, n := 0, len(sql)
	for i < n {
		switch c := sql[i]; {
		case c == '\'' || c == '"' || c == '`':
			i = copyQuoted(sql, i, &b)
		case c == '-' && i+1 < n && sql[i+1] == '-':
			for i < n && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			i += 2
			for i+1 < n && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			i += 2 // 跳过 "*/"；未闭合时 i 越界由外层条件兜住
		case c == ';':
			flush()
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	flush()
	return stmts
}

// copyQuoted 把 sql[i] 起的一个带引号字面量整体复制进 b，返回字面量
// 结束后的下标。引号未闭合时返回 len(sql)，语法错误交给数据库报错。
func copyQuoted(sql string, i int, b *strings.Builder) int {
	q := sql[i]
	b.WriteByte(q)
	i++
	for i < len(sql) {
		c := sql[i]
		b.WriteByte(c)
		i++
		switch {
		case c == '\\' && q != '`' && i < len(sql):
			b.WriteByte(sql[i]) // MySQL 反斜杠转义：带上下一个字符
			i++
		case c == q && i < len(sql) && sql[i] == q:
			b.WriteByte(q) // 相邻双写引号（''）是字面量内转义
			i++
		case c == q:
			return i
		}
	}
	return i
}
