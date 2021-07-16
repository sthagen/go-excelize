// Copyright 2016 - 2021 The excelize Authors. All rights reserved. Use of
// this source code is governed by a BSD-style license that can be found in
// the LICENSE file.
//
// Package excelize providing a set of functions that allow you to write to
// and read from XLSX / XLSM / XLTM files. Supports reading and writing
// spreadsheet documents generated by Microsoft Excel™ 2007 and later. Supports
// complex components by high compatibility, and provided streaming API for
// generating or reading data from a worksheet with huge amounts of data. This
// library needs Go version 1.15 or later.

package excelize

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
)

// NewFile provides a function to create new file by default template. For
// example:
//
//    f := NewFile()
//
func NewFile() *File {
	f := newFile()
	f.Pkg.Store("_rels/.rels", []byte(XMLHeader+templateRels))
	f.Pkg.Store("docProps/app.xml", []byte(XMLHeader+templateDocpropsApp))
	f.Pkg.Store("docProps/core.xml", []byte(XMLHeader+templateDocpropsCore))
	f.Pkg.Store("xl/_rels/workbook.xml.rels", []byte(XMLHeader+templateWorkbookRels))
	f.Pkg.Store("xl/theme/theme1.xml", []byte(XMLHeader+templateTheme))
	f.Pkg.Store("xl/worksheets/sheet1.xml", []byte(XMLHeader+templateSheet))
	f.Pkg.Store("xl/styles.xml", []byte(XMLHeader+templateStyles))
	f.Pkg.Store("xl/workbook.xml", []byte(XMLHeader+templateWorkbook))
	f.Pkg.Store("[Content_Types].xml", []byte(XMLHeader+templateContentTypes))
	f.SheetCount = 1
	f.CalcChain = f.calcChainReader()
	f.Comments = make(map[string]*xlsxComments)
	f.ContentTypes = f.contentTypesReader()
	f.Drawings = sync.Map{}
	f.Styles = f.stylesReader()
	f.DecodeVMLDrawing = make(map[string]*decodeVmlDrawing)
	f.VMLDrawing = make(map[string]*vmlDrawing)
	f.WorkBook = f.workbookReader()
	f.Relationships = sync.Map{}
	f.Relationships.Store("xl/_rels/workbook.xml.rels", f.relsReader("xl/_rels/workbook.xml.rels"))
	f.sheetMap["Sheet1"] = "xl/worksheets/sheet1.xml"
	ws, _ := f.workSheetReader("Sheet1")
	f.Sheet.Store("xl/worksheets/sheet1.xml", ws)
	f.Theme = f.themeReader()
	return f
}

// Save provides a function to override the spreadsheet with origin path.
func (f *File) Save() error {
	if f.Path == "" {
		return fmt.Errorf("no path defined for file, consider File.WriteTo or File.Write")
	}
	return f.SaveAs(f.Path)
}

// SaveAs provides a function to create or update to an spreadsheet at the
// provided path.
func (f *File) SaveAs(name string, opt ...Options) error {
	if len(name) > MaxFileNameLength {
		return ErrMaxFileNameLength
	}
	f.Path = name
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer file.Close()
	f.options = nil
	for _, o := range opt {
		f.options = &o
	}
	return f.Write(file)
}

// Write provides a function to write to an io.Writer.
func (f *File) Write(w io.Writer) error {
	_, err := f.WriteTo(w)
	return err
}

// WriteTo implements io.WriterTo to write the file.
func (f *File) WriteTo(w io.Writer) (int64, error) {
	if f.options != nil && f.options.Password != "" {
		buf, err := f.WriteToBuffer()
		if err != nil {
			return 0, err
		}
		return buf.WriteTo(w)
	}
	if err := f.writeDirectToWriter(w); err != nil {
		return 0, err
	}
	return 0, nil
}

// WriteToBuffer provides a function to get bytes.Buffer from the saved file. And it allocate space in memory. Be careful when the file size is large.
func (f *File) WriteToBuffer() (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	if err := f.writeToZip(zw); err != nil {
		return buf, zw.Close()
	}

	if f.options != nil && f.options.Password != "" {
		if err := zw.Close(); err != nil {
			return buf, err
		}
		b, err := Encrypt(buf.Bytes(), f.options)
		if err != nil {
			return buf, err
		}
		buf.Reset()
		buf.Write(b)
		return buf, nil
	}
	return buf, zw.Close()
}

// writeDirectToWriter provides a function to write to io.Writer.
func (f *File) writeDirectToWriter(w io.Writer) error {
	zw := zip.NewWriter(w)
	if err := f.writeToZip(zw); err != nil {
		zw.Close()
		return err
	}
	return zw.Close()
}

// writeToZip provides a function to write to zip.Writer
func (f *File) writeToZip(zw *zip.Writer) error {
	f.calcChainWriter()
	f.commentsWriter()
	f.contentTypesWriter()
	f.drawingsWriter()
	f.vmlDrawingWriter()
	f.workBookWriter()
	f.workSheetWriter()
	f.relsWriter()
	f.sharedStringsWriter()
	f.styleSheetWriter()

	for path, stream := range f.streams {
		fi, err := zw.Create(path)
		if err != nil {
			return err
		}
		var from io.Reader
		from, err = stream.rawData.Reader()
		if err != nil {
			stream.rawData.Close()
			return err
		}
		_, err = io.Copy(fi, from)
		if err != nil {
			return err
		}
		stream.rawData.Close()
	}
	var err error
	f.Pkg.Range(func(path, content interface{}) bool {
		if err != nil {
			return false
		}
		if _, ok := f.streams[path.(string)]; ok {
			return true
		}
		var fi io.Writer
		fi, err = zw.Create(path.(string))
		if err != nil {
			return false
		}
		_, err = fi.Write(content.([]byte))
		return true
	})

	return err
}
