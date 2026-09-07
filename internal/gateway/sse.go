package gateway

import (
	"bytes"
	"strings"
)

// SSESplitter SSE 流分割器：把上游字节流切分为事件 data 载荷，
// 供透传模式下旁路统计（不影响转发内容）。
// 规则：空行分隔事件；"data: X" 行为载荷；event: 行忽略（JSON 内含 type）。
type SSESplitter struct {
	onData  func(data []byte)
	lineBuf bytes.Buffer
	dataBuf bytes.Buffer
	hasData bool
}

// NewSSESplitter 构造分割器，onData 在每个事件完成时回调（含多条 data 行拼接，\n 分隔）。
func NewSSESplitter(onData func(data []byte)) *SSESplitter {
	return &SSESplitter{onData: onData}
}

// Write 喂入字节流；实现 io.Writer。
func (s *SSESplitter) Write(p []byte) (int, error) {
	for len(p) > 0 {
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			s.lineBuf.Write(p)
			return len(p), nil
		}
		s.lineBuf.Write(p[:idx])
		s.handleLine(s.lineBuf.Bytes())
		s.lineBuf.Reset()
		p = p[idx+1:]
	}
	return len(p), nil
}

// Flush 流结束时调用，处理无换行结尾的残余行。
func (s *SSESplitter) Flush() {
	if s.lineBuf.Len() > 0 {
		s.handleLine(s.lineBuf.Bytes())
		s.lineBuf.Reset()
	}
	s.finishEvent()
}

func (s *SSESplitter) handleLine(line []byte) {
	line = bytes.TrimSuffix(line, []byte("\r"))
	if len(line) == 0 {
		s.finishEvent()
		return
	}
	text := string(line)
	switch {
	case strings.HasPrefix(text, "data:"):
		payload := strings.TrimPrefix(text, "data:")
		payload = strings.TrimPrefix(payload, " ") // "data: X" 规范格式
		if s.hasData {
			s.dataBuf.WriteByte('\n')
		}
		s.dataBuf.WriteString(payload)
		s.hasData = true
	case strings.HasPrefix(text, ":"):
		// 注释/心跳行，忽略
	default:
		// event:/id:/retry: 等字段忽略（JSON 载荷内含 type）
	}
}

func (s *SSESplitter) finishEvent() {
	if s.hasData {
		data := make([]byte, s.dataBuf.Len())
		copy(data, s.dataBuf.Bytes())
		if string(data) != "[DONE]" {
			s.onData(data)
		}
		s.dataBuf.Reset()
		s.hasData = false
	}
}
