package render

import (
	"bytes"
	"fmt"
	"go/format"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strings"
	"text/template"

	"github.com/beevik/etree"
	"github.com/huangjunwen/sqlw/datasrc"
	"github.com/huangjunwen/sqlw/infos"
)

// Renderer is used for generating code.
type Renderer struct {
	// Options
	loader    *datasrc.Loader
	tmplFS    http.FileSystem
	stmtDir   string
	outputDir string
	outputPkg string
	whitelist map[string]struct{}
	blacklist map[string]struct{}

	// Runtime variables.
	db          *infos.DBInfo
	scanTypeMap ScanTypeMap
	templates   map[string]*template.Template
}

// NewRenderer create new Renderer.
func NewRenderer(opts ...Option) (*Renderer, error) {

	r := &Renderer{}
	for _, op := range opts {
		if err := op(r); err != nil {
			return nil, err
		}
	}

	if r.loader == nil {
		return nil, fmt.Errorf("Missing Loader")
	}
	if r.tmplFS == nil {
		return nil, fmt.Errorf("Missing FS")
	}
	if r.outputDir == "" {
		return nil, fmt.Errorf("Missing output directory")
	}
	if r.outputPkg == "" {
		r.outputPkg = path.Base(r.outputDir)
	}
	if len(r.whitelist) != 0 && len(r.blacklist) != 0 {
		return nil, fmt.Errorf("Only one of whitelist or blacklist can be specified")
	}

	return r, nil

}

func (r *Renderer) render(tmplName, fileName string, data interface{}) error {

	// Open template if not exists.
	tmpl := r.templates[tmplName]
	if tmpl == nil {
		tmplFile, err := r.tmplFS.Open(tmplName)
		if err != nil {
			return err
		}

		tmplContent, err := ioutil.ReadAll(tmplFile)
		if err != nil {
			return err
		}

		tmpl, err = template.New(tmplName).Funcs(r.funcMap()).Parse(string(tmplContent))
		if err != nil {
			return err
		}

		r.templates[tmplName] = tmpl
	}

	// Open output file.
	file, err := os.OpenFile(path.Join(r.outputDir, fileName), os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Render.
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, data); err != nil {
		return err
	}

	// Format.
	fmtBuf, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}

	// Write.
	_, err = file.Write(fmtBuf)
	if err != nil {
		return err
	}

	return nil

}

// Run generate code.
func (r *Renderer) Run() error {

	// Reset runtime variables.
	r.db = nil
	r.scanTypeMap = nil
	r.templates = make(map[string]*template.Template)

	// Load db
	db, err := infos.NewDBInfo(r.loader)
	if err != nil {
		return err
	}
	r.db = db

	// Parse manifest.
	manifestFile, err := r.tmplFS.Open("/manifest.json")
	if err != nil {
		return err
	}
	defer manifestFile.Close()

	manifest, err := NewManifest(manifestFile)
	if err != nil {
		return err
	}

	// Load scan type map.
	scanTypeMapFile, err := r.tmplFS.Open(manifest.ScanTypeMap)
	if err != nil {
		return err
	}
	defer scanTypeMapFile.Close()

	scanTypeMap, err := NewScanTypeMap(r.loader, scanTypeMapFile)
	if err != nil {
		return err
	}
	r.scanTypeMap = scanTypeMap

	// Render tables.
	for _, table := range r.db.Tables() {
		if len(r.whitelist) != 0 {
			if _, found := r.whitelist[table.TableName()]; !found {
				continue
			}
		} else if len(r.blacklist) != 0 {
			if _, found := r.blacklist[table.TableName()]; found {
				continue
			}
		}
		if err := r.render(manifest.Templates.Table, "table_"+table.TableName()+".go", map[string]interface{}{
			"PackageName": r.outputPkg,
			"Loader":      r.loader,
			"DB":          r.db,
			"Table":       table,
		}); err != nil {
			return err
		}
		if manifest.Templates.TableTest != "" {
			if err := r.render(manifest.Templates.TableTest, "table_"+table.TableName()+"_test.go", map[string]interface{}{
				"PackageName": r.outputPkg,
				"Loader":      r.loader,
				"DB":          r.db,
				"Table":       table,
			}); err != nil {
				return err
			}

		}
	}

	// Render statements.
	if r.stmtDir != "" {
		stmtFileInfos, err := ioutil.ReadDir(r.stmtDir)
		if err != nil {
			return err
		}
		for _, stmtFileInfo := range stmtFileInfos {
			if stmtFileInfo.IsDir() {
				continue
			}
			stmtFileName := stmtFileInfo.Name()
			if !strings.HasSuffix(stmtFileName, ".xml") {
				continue
			}
			doc := etree.NewDocument()
			if err := doc.ReadFromFile(path.Join(r.stmtDir, stmtFileName)); err != nil {
				return err
			}

			stmtInfos := []*infos.StmtInfo{}
			for _, elem := range doc.ChildElements() {
				stmtInfo, err := infos.NewStmtInfo(r.loader, r.db, elem)
				if err != nil {
					return err
				}
				stmtInfos = append(stmtInfos, stmtInfo)
			}

			if err := r.render(manifest.Templates.Stmt, "stmt_"+stripSuffix(stmtFileName)+".go", map[string]interface{}{
				"PackageName": r.outputPkg,
				"Loader":      r.loader,
				"DB":          r.db,
				"Stmts":       stmtInfos,
			}); err != nil {
				return err
			}
			if manifest.Templates.StmtTest != "" {
				if err := r.render(manifest.Templates.StmtTest, "stmt_"+stripSuffix(stmtFileName)+"_test.go", map[string]interface{}{
					"PackageName": r.outputPkg,
					"Loader":      r.loader,
					"DB":          r.db,
					"Stmts":       stmtInfos,
				}); err != nil {
					return err
				}

			}

		}
	}

	// Render extra files.
	for _, tmplName := range manifest.Templates.Extra {
		// Render.
		fileName := "extra_" + stripSuffix(tmplName) + ".go"
		if err := r.render(tmplName, fileName, map[string]interface{}{
			"PackageName": r.outputPkg,
			"Loader":      r.loader,
			"DB":          r.db,
		}); err != nil {
			return err
		}
	}

	return nil
}

func stripSuffix(s string) string {
	i := strings.LastIndexByte(s, '.')
	if i < 0 {
		return s
	}
	return s[:i]
}
