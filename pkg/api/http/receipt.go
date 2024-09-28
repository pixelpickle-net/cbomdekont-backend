package http

import (
	"strings"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/textract/types"
)

type FieldStrategy struct {
	Key      string `json:"key"`
	Strategy string `json:"strategy"`
}

type DocumentSchema struct {
	Type   string                   `json:"type"`
	Fields map[string]FieldStrategy `json:"fields"`
}

type ReceiptParser struct {
	blocks []types.Block
	schema DocumentSchema
}

func NewReceiptParser(blocks []types.Block, schema DocumentSchema) *ReceiptParser {
	return &ReceiptParser{
		blocks: blocks,
		schema: schema,
	}
}

func (p *ReceiptParser) Parse() ExtractedInfo {
	extractedInfo := make(ExtractedInfo)
	fmt.Println("Parsing document with schema:", p.schema)
	fmt.Println("Total blocks:", len(p.blocks))
	
	for field, strategy := range p.schema.Fields {
		fmt.Printf("Searching for field: %s with key: %s and strategy: %s\n", field, strategy.Key, strategy.Strategy)
		value := p.findFieldValue(strategy)
		if value != "" {
			extractedInfo[field] = value
			fmt.Printf("Found value for %s: %s\n", field, value)
		} else {
			fmt.Printf("Could not find value for field: %s\n", field)
		}
	}
	
	if len(extractedInfo) == 0 {
		fmt.Println("No information extracted. Printing all blocks:")
		for _, block := range p.blocks {
			if block.Text != nil {
				fmt.Printf("BlockType: %s, Text: %s\n", block.BlockType, *block.Text)
			}
		}
	}
	
	return extractedInfo
}

func (p *ReceiptParser) findFieldValue(strategy FieldStrategy) string {
	switch strategy.Strategy {
	case "keyValueSet":
		return p.findKeyValueSet(strategy.Key)
	case "nextLine":
		return p.findNextLine(strategy.Key)
	case "sameLine":
		return p.findSameLine(strategy.Key)
	case "table":
		return p.findInTable(strategy.Key)
	default:
		return ""
	}
}

func (p *ReceiptParser) findKeyValueSet(key string) string {
	fmt.Printf("Searching for key: %s in KEY_VALUE_SET\n", key)
	for _, block := range p.blocks {
		if block.BlockType == types.BlockTypeKeyValueSet && len(block.EntityTypes) > 0 && block.EntityTypes[0] == types.EntityTypeKey {
			if block.Text != nil {
				fmt.Printf("Found KEY block with text: %s\n", *block.Text)
				if *block.Text == key {
					fmt.Printf("Key match found for: %s\n", key)
					for _, relationship := range block.Relationships {
						if relationship.Type == types.RelationshipTypeValue {
							for _, valueId := range relationship.Ids {
								valueBlock := p.findBlockById(valueId)
								if valueBlock != nil && valueBlock.Text != nil {
									fmt.Printf("Found VALUE for %s: %s\n", key, *valueBlock.Text)
									return *valueBlock.Text
								}
							}
						}
					}
				}
			}
		}
	}
	fmt.Printf("No value found for key: %s\n", key)
	return ""
}

func (p *ReceiptParser) isKeyValueSet(block types.Block, key string) bool {
	return block.BlockType == types.BlockTypeKeyValueSet &&
		block.EntityTypes != nil &&
		len(block.EntityTypes) > 0 &&
		block.EntityTypes[0] == types.EntityTypeKey &&
		block.Text != nil &&
		*block.Text == key
}

func (p *ReceiptParser) getValueFromKeyValueSet(block types.Block) string {
	for _, relationship := range block.Relationships {
		if relationship.Type == types.RelationshipTypeValue {
			for _, valueId := range relationship.Ids {
				valueBlock := p.findBlockById(valueId)
				if valueBlock != nil && valueBlock.Text != nil {
					return *valueBlock.Text
				}
			}
		}
	}
	return ""
}

func (p *ReceiptParser) findNextLine(key string) string {
	for i, block := range p.blocks {
		if block.BlockType == types.BlockTypeLine && block.Text != nil && *block.Text == key {
			if i+1 < len(p.blocks) {
				nextBlock := p.blocks[i+1]
				if nextBlock.BlockType == types.BlockTypeLine && nextBlock.Text != nil {
					return *nextBlock.Text
				}
			}
		}
	}
	return ""
}

func (p *ReceiptParser) findSameLine(key string) string {
	for _, block := range p.blocks {
		if block.BlockType == types.BlockTypeLine && block.Text != nil && strings.Contains(*block.Text, key) {
			parts := strings.SplitN(*block.Text, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func (p *ReceiptParser) findInTable(key string) string {
	for _, block := range p.blocks {
		if block.BlockType == types.BlockTypeCell && block.Text != nil && strings.Contains(*block.Text, key) {
			if block.RowIndex != nil && block.ColumnIndex != nil {
				return p.getValueFromNextCell(*block.RowIndex, *block.ColumnIndex)
			}
		}
	}
	return ""
}

func (p *ReceiptParser) findBlockById(id string) *types.Block {
	for _, block := range p.blocks {
		if block.Id != nil && *block.Id == id {
			return &block
		}
	}
	return nil
}

func (p *ReceiptParser) getValueFromNextCell(rowIndex, columnIndex int32) string {
	for _, block := range p.blocks {
		if block.BlockType == types.BlockTypeCell &&
			block.RowIndex != nil && *block.RowIndex == rowIndex &&
			block.ColumnIndex != nil && *block.ColumnIndex == columnIndex+1 &&
			block.Text != nil {
			return *block.Text
		}
	}
	return ""
}
