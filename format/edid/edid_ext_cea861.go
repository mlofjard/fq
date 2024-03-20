package edid

import (
	"github.com/wader/fq/format"
	"github.com/wader/fq/pkg/decode"
	"github.com/wader/fq/pkg/interp"
	"github.com/wader/fq/pkg/scalar"
)

func init() {
	interp.RegisterFormat(
		format.EDID_Ext_CEA861,
		&decode.Format{
			Description: "EDID CEA-861 Series Timing Extension",
			DecodeFn:    decodeCEAExtension,
			Groups:      []*decode.Group{format.EDID_Extension},
		})
}

func decodeCEAExtension(d *decode.D) any {
	d.FieldU8("tag", scalar.UintHex, d.UintAssert(0x02))

	d.FieldU8("revision")
	offset := d.FieldU8("offset")
	d.FieldU8("reserved")

	d.FieldRawLen("padding", (int64(offset)-4)*8)

	DetailedDescriptor(d, "third_timing_descriptor", 1)
	DetailedDescriptor(d, "fourth_timing_descriptor", 1)

	d.FieldRawLen("data", (123-32-int64(offset))*8)

	sum := CalcSum(d.BytesRange(0, 127))
	d.FieldU8("checksum", d.UintValidate(uint64(0-sum)), scalar.UintHex)
	return nil
}
