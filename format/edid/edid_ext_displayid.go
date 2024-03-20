package edid

import (
	"fmt"

	"github.com/wader/fq/format"
	"github.com/wader/fq/pkg/decode"
	"github.com/wader/fq/pkg/interp"
	"github.com/wader/fq/pkg/scalar"
)

func init() {
	interp.RegisterFormat(
		format.EDID_Ext_DisplayID,
		&decode.Format{
			Description: "EDID DisplayID Extension",
			DecodeFn:    decodeDisplayIDExtension,
			Groups:      []*decode.Group{format.EDID_Extension},
		})
}

var tagMap = scalar.UintMapDescription{
	0x00: "Product Identification Data Block",
	0x01: "Display Parameters Data Block",
	0x02: "Color Characteristics",
	0x03: "Type I Timing - Detailed",
	0x04: "Type II Timing - Detailed",
	0x05: "Type III Timing - Short",
	0x06: "Type IV Timing - DMT ID Code",
	0x07: "VESA Timing Standard",
	0x08: "CEA Timing Standard",
	0x09: "Video Timing Range Limits",
	0x0a: "Product Serial Number",
	0x0b: "General Purpose ASCII String",
	0x0c: "Display Device Data",
	0x0d: "Interface Power Sequencing Data Block",
	0x0e: "Transfer Characteristics Data Block",
	0x0f: "Display Interface Data Block",
	0x10: "Stereo Display Interface Data Block",
	0x11: "Type V Timing - Short",
	0x12: "Tiled Display Topology Data Block",
	0x13: "Type VI Timing - Detailed",
	0x7f: "Vendor Specific Data Block",
}

var aspectMap = scalar.UintMapSymStr{
	0: "1:1",
	1: "5:4",
	2: "4:3",
	3: "15:9",
	4: "16:9",
	5: "16:10",
	6: "64:27",
	7: "256:135",
	8: "undefined",
}

func decodeTagTimingIDetailed(d *decode.D) {
	d.FieldU8("revision")
	pBytes := d.FieldU8("payload_bytes")
	numberOfTimings := pBytes / 20
	for i := 0; i < int(numberOfTimings); i++ {
		clkStart := d.Pos()
		clk1 := d.U8()
		clk2 := d.U8()
		clk3 := d.U8()
		clkLen := d.Pos() - clkStart
		pixelClock := clk1 + (clk2 << 8) + (clk3 << 16)
		FieldValueUintAddr(d, "pixel_clock", pixelClock, clkStart, clkLen, scalar.UintActualAdd(1), pixelClockMapper, descUintMapper("MHz"))

		d.FieldBool("preferred_timing")
		d.FieldU2("3d_stereo_support", scalar.UintMapSymStr{0: "no_stereo", 1: "always_stereo", 2: "switchable_stereo"})
		d.FieldU1("scan_type", scalar.UintMapSymStr{0: "progressive", 1: "interlaced"})
		d.FieldU4("aspect_ratio", aspectMap)

		d.FieldU16LE("horizontal_active_image_pixels", scalar.UintActualAdd(1))
		d.FieldU16LE("horizontal_blank_pixels", scalar.UintActualAdd(1))

		hfpStart := d.Pos()
		hfp1 := d.U8() // horizontal_front_porch lower 8 bits
		d.FieldU1("horizontal_sync_polarity", scalar.UintMapSymStr{0: "negative", 1: "positive"})
		hfp2 := d.U7() // horizontal_front_porch upper 7 bits
		hfp := hfp1 + (hfp2 << 8)
		FieldValueUintAddr(d, "horizontal_front_porch", hfp, hfpStart, 16, scalar.UintActualAdd(1), descUintMapper("pixels"))

		d.FieldU16LE("horizontal_sync_width", scalar.UintActualAdd(1), descUintMapper("pixels"))

		d.FieldU16LE("vertical_active_image_lines", scalar.UintActualAdd(1))
		d.FieldU16LE("vertical_blank_lines", scalar.UintActualAdd(1))

		vfpStart := d.Pos()
		vfp1 := d.U8() // vertical_front_porch lower 8 bits
		d.FieldU1("vertical_sync_polarity", scalar.UintMapSymStr{0: "negative", 1: "positive"})
		vfp2 := d.U7() // vertical_front_porch upper 7 bits
		vfp := vfp1 + (vfp2 << 8)
		FieldValueUintAddr(d, "vertical_front_porch", vfp, vfpStart, 16, scalar.UintActualAdd(1), descUintMapper("lines"))

		d.FieldU16LE("vertical_sync_width", scalar.UintActualAdd(1), descUintMapper("lines"))
	}
}

func decodeDisplayID(d *decode.D) {
	d.FieldU4("display_id_version")
	d.FieldU4("display_id_revision")

	byteCount := int(d.FieldU8("bytes_of_data"))

	id := d.FieldU8("display_product_type_identifier")
	ext := d.FieldU8("extension_count")

	if id == 0 && ext == 0 {
		d.FieldValueBool("is_an_extension", true)
	}

	// decode data blocks
	d.FieldArray("data_blocks", func(d *decode.D) {
		dataStart := d.Pos()
		for d.Pos() < int64((5+byteCount)*8) { // TODO: 5 should be 4 when this is its own format since no external tag will exist
			tag := d.PeekUintBits(8)
			if tag == 0 && dataStart != d.Pos() { // tag 0x00 is only allowed to be the first tag
				break
			} else {
				d.FieldStruct("data_block", func(d *decode.D) {
					tag := d.FieldU8("tag", tagMap, scalar.UintHex)

					switch tag {
					case 0x03:
						decodeTagTimingIDetailed(d)
					default:
						d.FieldU5("block_header")
						d.FieldU3("revision")
						pBytes := d.FieldU8("payload_bytes")
						d.FieldRawLen("payload", int64(pBytes)*8)
					}
				})
			}
		}
	})

	fmt.Printf("bits left %d", d.BitsLeft()/8)
	if d.BitsLeft()-8 > 0 {
		d.FieldRawLen("padding", d.BitsLeft()-8)
	}

	sum := CalcSum(d.BytesRange(8, 4+byteCount))
	d.FieldU8("checksum", d.UintValidate(uint64(0-sum)), scalar.UintHex)
}

func decodeDisplayIDExtension(d *decode.D) any {
	d.FieldU8("tag", scalar.UintHex, d.UintAssert(0x70))

	// TODO: The insides of this extension block is actually its own format, but
	// without extensions of its own. In the future this could be extracted and
	// extension support added, and then called from within here.

	d.FieldStruct("data", func(d *decode.D) {
		d.FramedFn(126*8, decodeDisplayID)
	})

	sum := CalcSum(d.BytesRange(0, 127))
	d.FieldU8("checksum", d.UintValidate(uint64(0-sum)), scalar.UintHex)
	return nil
}
