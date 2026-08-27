// Package output — вывод команд: единый JSON-конверт по умолчанию и
// человекочитаемый режим по --human.
//
// Потребитель по умолчанию — агент, а не человек, поэтому JSON здесь основной
// режим, а не опция.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// SchemaVersion — версия конверта; растёт, когда меняется форма полей.
const SchemaVersion = 1

// Failure — ошибка в машинном виде. Kind — короткий класс (auth, network,
// usage, not_found, timeout, api), по нему принимают решение, не читая текст.
type Failure struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// Envelope — то, что печатается в режиме JSON.
//
// Поле Exit дублирует код выхода намеренно: команду агент может запускать
// фоном и читать результат из файла, где кода выхода уже нет.
type Envelope struct {
	V       int      `json:"v"`
	Command string   `json:"command"`
	Exit    int      `json:"exit"`
	Data    any      `json:"data"`
	Warning []string `json:"warning,omitempty"`
	Error   *Failure `json:"error"`
}

type Printer struct {
	Human bool
	Out   io.Writer
	Err   io.Writer

	warnings []string
}

func New(human bool) *Printer {
	return &Printer{Human: human, Out: os.Stdout, Err: os.Stderr}
}

// Warn копит предупреждения: в JSON они уезжают полем warning, человеку
// печатаются перед результатом. Это не ошибки — команда продолжает работу.
func (p *Printer) Warn(format string, a ...any) {
	p.warnings = append(p.warnings, fmt.Sprintf(format, a...))
}

// Result печатает результат и возвращает код выхода, чтобы вызов сворачивался
// в одну строку: return p.Result(...). В режиме --human вместо конверта
// зовётся human — он пишет то, что удобно читать глазами.
func (p *Printer) Result(command string, code int, data any, human func(io.Writer)) int {
	if p.Human {
		for _, w := range p.warnings {
			fmt.Fprintln(p.Err, "warn:", w)
		}
		if human != nil {
			human(p.Out)
		}
		return code
	}
	p.encode(Envelope{
		V:       SchemaVersion,
		Command: command,
		Exit:    code,
		Data:    data,
		Warning: p.warnings,
	})
	return code
}

// Fail печатает ошибку и возвращает код выхода. В человеческом режиме текст
// уходит в stderr, в машинном — тем же конвертом в stdout: разбирающая
// сторона не должна читать два потока, чтобы понять исход.
func (p *Printer) Fail(command string, code int, kind, format string, a ...any) int {
	msg := fmt.Sprintf(format, a...)
	if p.Human {
		for _, w := range p.warnings {
			fmt.Fprintln(p.Err, "warn:", w)
		}
		fmt.Fprintln(p.Err, msg)
		return code
	}
	p.encode(Envelope{
		V:       SchemaVersion,
		Command: command,
		Exit:    code,
		Warning: p.warnings,
		Error:   &Failure{Kind: kind, Message: msg},
	})
	return code
}

func (p *Printer) encode(e Envelope) {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	// Без этого амперсанд в ссылках уезжает в &amp; и ссылка перестаёт
	// открываться копипастой.
	enc.SetEscapeHTML(false)
	_ = enc.Encode(e)
}
