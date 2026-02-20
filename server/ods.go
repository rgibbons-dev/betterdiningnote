package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// writeODS writes an ODS (OpenDocument Spreadsheet) file to the writer.
// ODS is a zip archive containing XML files.
func writeODS(w io.Writer, rows [][]string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	// mimetype must be first entry, stored (not compressed)
	mw, err := zw.CreateHeader(&zip.FileHeader{
		Name:   "mimetype",
		Method: zip.Store,
	})
	if err != nil {
		return err
	}
	mw.Write([]byte("application/vnd.oasis.opendocument.spreadsheet"))

	// META-INF/manifest.xml
	manifestW, err := zw.Create("META-INF/manifest.xml")
	if err != nil {
		return err
	}
	manifestW.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.2">
  <manifest:file-entry manifest:media-type="application/vnd.oasis.opendocument.spreadsheet" manifest:version="1.2" manifest:full-path="/"/>
  <manifest:file-entry manifest:media-type="text/xml" manifest:full-path="content.xml"/>
</manifest:manifest>`))

	// content.xml
	contentW, err := zw.Create("content.xml")
	if err != nil {
		return err
	}

	contentW.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<office:document-content
  xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"
  xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"
  xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0"
  office:version="1.2">
<office:body>
<office:spreadsheet>
<table:table table:name="Meals">
`))

	for _, row := range rows {
		contentW.Write([]byte("<table:table-row>\n"))
		for _, cell := range row {
			escaped := xmlEscape(cell)
			contentW.Write([]byte(fmt.Sprintf(`<table:table-cell office:value-type="string"><text:p>%s</text:p></table:table-cell>`+"\n", escaped)))
		}
		contentW.Write([]byte("</table:table-row>\n"))
	}

	contentW.Write([]byte(`</table:table>
</office:spreadsheet>
</office:body>
</office:document-content>`))

	return nil
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
