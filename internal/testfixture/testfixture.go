// Package testfixture generates minimal real input files for end-to-end tests.
package testfixture

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
)

// WritePDF writes a minimal valid one-page PDF to path.
func WritePDF(path string) error {
	var buf bytes.Buffer
	objects := []string{}
	add := func(s string) int {
		objects = append(objects, s)
		return len(objects)
	}
	catalog := add("<< /Type /Catalog /Pages 2 0 R >>")
	pages := add("<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	page := add("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>")
	content := add("<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (Hello) Tj ET\nendstream")
	font := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	_ = catalog
	_ = pages
	_ = page
	_ = content
	_ = font

	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, o := range objects {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xrefPos := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	fmt.Fprintln(&buf, "0000000000 65535 f ")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\n", len(objects)+1)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefPos)
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// WriteDocx writes a minimal valid docx (zip container) to path.
func WriteDocx(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Hello</w:t></w:r></w:p></w:body></w:document>`,
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return err
		}
	}
	return zw.Close()
}
