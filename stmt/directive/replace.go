package directive

import (
	"database/sql"
	"fmt"

	"github.com/beevik/etree"

	"github.com/huangjunwen/sqlw/dbctx"
	"github.com/huangjunwen/sqlw/stmt"
)

type replaceDirective struct {
	origin string
	with   string
}

func (d *replaceDirective) Initialize(ctx *dbctx.DBContext, statement *stmt.StatementInfo, tok etree.Token) error {
	elem := tok.(*etree.Element)
	with := elem.SelectAttrValue("with", "")
	if with == "" {
		return fmt.Errorf("Missing 'with' attribute in <replace> directive")
	}
	d.origin = elem.Text()
	d.with = with
	return nil
}

func (d *replaceDirective) Generate() (string, error) {
	return d.with, nil
}

func (d *replaceDirective) GenerateQuery() (string, error) {
	return d.origin, nil
}

func (d *replaceDirective) ProcessQueryResult(resultColumnNames *[]string, resultColumnTypes *[]*sql.ColumnType) error {
	return nil
}

func init() {
	stmt.RegistDirectiveFactory(func() stmt.Directive {
		return &replaceDirective{}
	}, "replace")
}
