package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/textract"
	"github.com/aws/aws-sdk-go-v2/service/textract/types"
	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

const (
	Document = "document"
)

// TextractResponse represents the overall structure of the Textract analysis result
type TextractResponse struct {
	DocumentMetadata DocumentMetadata `json:"DocumentMetadata"`
	Blocks           []Block          `json:"Blocks"`
}

// DocumentMetadata contains metadata about the analyzed document
type DocumentMetadata struct {
	Pages int `json:"Pages"`
}

// Block represents a single block of information from the Textract analysis
type Block struct {
	BlockType       string         `json:"BlockType"`
	Confidence      float64        `json:"Confidence"`
	Text            string         `json:"Text,omitempty"`
	RowIndex        int            `json:"RowIndex,omitempty"`
	ColumnIndex     int            `json:"ColumnIndex,omitempty"`
	RowSpan         int            `json:"RowSpan,omitempty"`
	ColumnSpan      int            `json:"ColumnSpan,omitempty"`
	Geometry        Geometry       `json:"Geometry"`
	Id              string         `json:"Id"`
	Relationships   []Relationship `json:"Relationships,omitempty"`
	EntityTypes     []string       `json:"EntityTypes,omitempty"`
	SelectionStatus string         `json:"SelectionStatus,omitempty"`
}

// Geometry represents the position of a block on the document
type Geometry struct {
	BoundingBox BoundingBox `json:"BoundingBox"`
	Polygon     []Point     `json:"Polygon"`
}

// BoundingBox represents the bounding box of a block
type BoundingBox struct {
	Width  float64 `json:"Width"`
	Height float64 `json:"Height"`
	Left   float64 `json:"Left"`
	Top    float64 `json:"Top"`
}

// Point represents a single point in the polygon
type Point struct {
	X float64 `json:"X"`
	Y float64 `json:"Y"`
}

// Relationship represents a relationship between blocks
type Relationship struct {
	Type string   `json:"Type"`
	Ids  []string `json:"Ids"`
}

// ExtractedInfo represents the specific information we want to extract
type ExtractedInfo map[string]string

type AWSConfig struct {
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	Region          string `mapstructure:"region"`
}

type AWSService struct {
	textractClient *textract.Client
	logger         *zap.Logger
	schemas        map[string]DocumentSchema
}

func NewAWSService(logger *zap.Logger, cfg *AWSConfig, schemaFile string) (*AWSService, error) {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
		config.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, err
	}

	textractClient := textract.NewFromConfig(awsCfg)

	schemas, err := loadSchemas(schemaFile)
	if err != nil {
		return nil, err
	}

	return &AWSService{
		textractClient: textractClient,
		logger:         logger,
		schemas:        schemas,
	}, nil
}
func loadSchemas(schemaFile string) (map[string]DocumentSchema, error) {
	f, err := os.Open(schemaFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var schemas map[string]DocumentSchema
	err = json.Unmarshal(data, &schemas)
	if err != nil {
		return nil, err
	}

	return schemas, nil
}

func (s *Server) testTextractorHandler(c fiber.Ctx) error {
	// Get the file from form data
	file, err := c.FormFile(Document)
	if err != nil {
		s.logger.Error("Failed to get file from form data", zap.Error(err))
		return fiber.NewError(fiber.StatusBadRequest, "Failed to get file from form data")
	}

	// Get the document type from form data
	docType := c.FormValue("docType")
	if docType == "" {
		s.logger.Error("Document type not provided")
		return fiber.NewError(fiber.StatusBadRequest, "Document type not provided")
	}

	// Open the file
	fileContent, err := file.Open()
	if err != nil {
		s.logger.Error("Failed to open file", zap.Error(err))
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to open file"})
	}
	defer func(fileContent multipart.File) {
		err := fileContent.Close()
		if err != nil {
			s.logger.Error("Failed to close file", zap.Error(err))
		}
	}(fileContent)

	// Read the file content
	fileBytes, err := io.ReadAll(fileContent)
	if err != nil {
		s.logger.Error("Failed to read file content", zap.Error(err))
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to read file content"})
	}

	// Create Textract input
	input := &textract.AnalyzeDocumentInput{
		Document: &types.Document{
			Bytes: fileBytes,
		},
		FeatureTypes: []types.FeatureType{
			types.FeatureTypeForms,
			types.FeatureTypeTables,
		},
	}

	// Call Textract service
	rawResult, err := s.awsService.textractClient.AnalyzeDocument(c.Context(), input)
	if err != nil {
		s.logger.Error("Failed to analyze document with Textract", zap.Error(err))
		return c.Status(fiber.StatusInternalServerError).JSON(BaseResponse{
			Success: false,
			Message: "Failed to analyze document",
		})
	}

	// Ham Textract sonucunu loglayalım
	s.logger.Debug("Raw Textract result", zap.Any("result", rawResult))

	// Extract information based on the document type
	extractedInfo, err := s.awsService.extractInfo(rawResult.Blocks, docType)
	if err != nil {
		s.logger.Error("Failed to extract information", zap.Error(err))
		return c.Status(fiber.StatusInternalServerError).JSON(BaseResponse{
			Success: false,
			Message: "Failed to extract information",
			Data:    rawResult, // Ham veriyi de dönelim
		})
	}

	// Hem extract edilmiş bilgiyi hem de ham veriyi döndürelim
	return c.Status(fiber.StatusOK).JSON(BaseResponse{
		Success: true,
		Message: "Information extracted successfully",
		Data: fiber.Map{
			"extractedInfo": extractedInfo,
		},
	})
}

func (s *AWSService) extractInfo(blocks []types.Block, docType string) (ExtractedInfo, error) {
	schema, ok := s.schemas[docType]
	if !ok {
		return nil, fmt.Errorf("schema not found for document type %s", docType)
	}

	parser := NewReceiptParser(blocks, schema)
	extractedInfo := parser.Parse()

	// Hata ayıklama için log ekleyelim
	s.logger.Debug("Extracted info", zap.Any("info", extractedInfo))

	// Eğer hiçbir bilgi çıkarılamadıysa, hata döndür
	if len(extractedInfo) == 0 {
		// Ham veriyi loglamak için
		s.logger.Debug("Raw Textract blocks", zap.Any("blocks", blocks))
		return nil, fmt.Errorf("no information could be extracted from the document")
	}

	return extractedInfo, nil
}

func (s *AWSService) findFieldValue(blocks []types.Block, key string) string {
	for i, block := range blocks {
		if block.BlockType == types.BlockTypeKeyValueSet && block.EntityTypes != nil && len(block.EntityTypes) > 0 && block.EntityTypes[0] == types.EntityTypeKey {
			if block.Text != nil && *block.Text == key {
				// Try to find the value using relationships first
				for _, relationship := range block.Relationships {
					if relationship.Type == types.RelationshipTypeValue {
						for _, valueId := range relationship.Ids {
							valueBlock := s.findBlockById(blocks, valueId)
							if valueBlock != nil && valueBlock.Text != nil {
								return *valueBlock.Text
							}
						}
					}
				}

				// If no value found through relationships, check the next block
				if i+1 < len(blocks) {
					nextBlock := blocks[i+1]
					if nextBlock.BlockType == types.BlockTypeLine && nextBlock.Text != nil {
						return *nextBlock.Text
					}
				}
			}
		} else if block.BlockType == types.BlockTypeLine && block.Text != nil {
			// This is the approach from the previous implementation
			if *block.Text == key {
				if i+1 < len(blocks) {
					nextBlock := blocks[i+1]
					if nextBlock.BlockType == types.BlockTypeLine && nextBlock.Text != nil {
						return *nextBlock.Text
					}
				}
				break
			}
		}
	}
	return ""
}

func (s *AWSService) findBlockById(blocks []types.Block, id string) *types.Block {
	for _, block := range blocks {
		if block.Id != nil && *block.Id == id {
			return &block
		}
	}
	return nil
}
