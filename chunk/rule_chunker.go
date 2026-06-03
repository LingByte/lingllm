package chunk

import (
	"context"
	"regexp"
	"strings"
)

// Copyright (c) 2026 LingByte. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0

const (
	DefaultRuleChunkMaxChars = 1200
	DefaultRuleChunkMinChars = 80
)

// StructuredRuleChunker 结构化文档规则分块器
type StructuredRuleChunker struct {
	config *Config
}

// NewStructuredRuleChunker 创建结构化规则分块器
func NewStructuredRuleChunker(cfg *Config) *StructuredRuleChunker {
	return &StructuredRuleChunker{config: cfg}
}

func (c *StructuredRuleChunker) Provider() string {
	return "rules_structured"
}

func (c *StructuredRuleChunker) Chunk(ctx context.Context, text string, opts *ChunkOptions) ([]Chunk, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrEmptyText
	}

	maxChars := DefaultRuleChunkMaxChars
	minChars := DefaultRuleChunkMinChars
	overlap := int(float64(maxChars) * 0.12)
	title := ""

	if opts != nil {
		if opts.MaxChars > 0 {
			maxChars = opts.MaxChars
		}
		if opts.MinChars > 0 {
			minChars = opts.MinChars
		}
		if opts.OverlapChars >= 0 {
			overlap = opts.OverlapChars
		}
		title = strings.TrimSpace(opts.DocumentTitle)
	}

	if maxChars <= 0 {
		return nil, ErrInvalidChunkOpt
	}
	if overlap < 0 {
		overlap = 0
	}

	// 按标题分割
	sections := splitByHeadings(text)
	if len(sections) == 0 {
		sections = []headingSection{{Heading: "", Body: text}}
	}

	var out []Chunk
	for _, sec := range sections {
		secTitle := strings.TrimSpace(sec.Heading)
		if secTitle == "" {
			secTitle = title
		} else if title != "" {
			secTitle = title + " / " + secTitle
		}
		chunks := chunkSectionBody(sec.Body, secTitle, maxChars, minChars)
		out = append(out, chunks...)
	}

	out = applySentenceOverlap(out, overlap)
	out = dropTinyTrailing(out, minChars)

	if len(out) == 0 {
		return nil, ErrNoChunks
	}

	for i := range out {
		out[i].Index = i
	}

	return out, nil
}

// TableKVChunker 表格/键值对分块器
type TableKVChunker struct {
	config *Config
}

// NewTableKVChunker 创建表格/键值对分块器
func NewTableKVChunker(cfg *Config) *TableKVChunker {
	return &TableKVChunker{config: cfg}
}

func (c *TableKVChunker) Provider() string {
	return "rules_table_kv"
}

func (c *TableKVChunker) Chunk(ctx context.Context, text string, opts *ChunkOptions) ([]Chunk, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrEmptyText
	}

	maxChars := DefaultRuleChunkMaxChars

	if opts != nil {
		if opts.MaxChars > 0 {
			maxChars = opts.MaxChars
		}
	}

	// 简单的表格/键值对分块：按空行分割
	records := strings.Split(text, "\n\n")
	var chunks []Chunk

	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}

		if len(record) <= maxChars {
			chunks = append(chunks, Chunk{
				Text:     record,
				Metadata: make(map[string]interface{}),
			})
		} else {
			// 如果单个记录太大，按行分割
			lines := strings.Split(record, "\n")
			var currentChunk strings.Builder

			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				if currentChunk.Len()+len(line)+1 > maxChars {
					if currentChunk.Len() > 0 {
						chunks = append(chunks, Chunk{
							Text:     currentChunk.String(),
							Metadata: make(map[string]interface{}),
						})
						currentChunk.Reset()
					}
				}

				if currentChunk.Len() > 0 {
					currentChunk.WriteString("\n")
				}
				currentChunk.WriteString(line)
			}

			if currentChunk.Len() > 0 {
				chunks = append(chunks, Chunk{
					Text:     currentChunk.String(),
					Metadata: make(map[string]interface{}),
				})
			}
		}
	}

	if len(chunks) == 0 {
		return nil, ErrNoChunks
	}

	for i := range chunks {
		chunks[i].Index = i
	}

	return chunks, nil
}

// 辅助函数

type headingSection struct {
	Heading string
	Body    string
}

func splitByHeadings(text string) []headingSection {
	// 简单的标题检测：以 # 开头的行
	headingRegex := regexp.MustCompile(`(?m)^#+\s+(.+)$`)
	matches := headingRegex.FindAllStringSubmatchIndex(text, -1)

	if len(matches) == 0 {
		return nil
	}

	var sections []headingSection
	for i, match := range matches {
		heading := text[match[2]:match[3]]

		var body string
		if i+1 < len(matches) {
			body = text[match[1]:matches[i+1][0]]
		} else {
			body = text[match[1]:]
		}

		sections = append(sections, headingSection{
			Heading: heading,
			Body:    body,
		})
	}

	return sections
}

func chunkSectionBody(body, title string, maxChars, minChars int) []Chunk {
	// 按段落分割
	paragraphs := strings.Split(body, "\n\n")
	var chunks []Chunk
	var currentChunk strings.Builder

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		if currentChunk.Len()+len(para)+2 > maxChars {
			if currentChunk.Len() > minChars {
				chunks = append(chunks, Chunk{
					Title:    title,
					Text:     currentChunk.String(),
					Metadata: make(map[string]interface{}),
				})
				currentChunk.Reset()
			}
		}

		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n\n")
		}
		currentChunk.WriteString(para)
	}

	if currentChunk.Len() > minChars {
		chunks = append(chunks, Chunk{
			Title:    title,
			Text:     currentChunk.String(),
			Metadata: make(map[string]interface{}),
		})
	}

	return chunks
}

func applySentenceOverlap(chunks []Chunk, overlapChars int) []Chunk {
	if overlapChars <= 0 || len(chunks) < 2 {
		return chunks
	}

	// 简单的重叠实现：在分块之间添加最后 overlapChars 个字符
	for i := 1; i < len(chunks); i++ {
		prevText := chunks[i-1].Text
		if len(prevText) > overlapChars {
			overlap := prevText[len(prevText)-overlapChars:]
			chunks[i].Text = overlap + "\n" + chunks[i].Text
		}
	}

	return chunks
}

func dropTinyTrailing(chunks []Chunk, minChars int) []Chunk {
	if len(chunks) == 0 {
		return chunks
	}

	// 删除太小的尾部分块
	for len(chunks) > 1 && len(chunks[len(chunks)-1].Text) < minChars {
		// 合并到前一个分块
		if len(chunks) >= 2 {
			chunks[len(chunks)-2].Text += "\n" + chunks[len(chunks)-1].Text
		}
		chunks = chunks[:len(chunks)-1]
	}

	return chunks
}
