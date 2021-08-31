package batch

import (
	"bytes"
	"fmt"
	"matrixone/pkg/container/vector"
	"matrixone/pkg/vm/process"
)

func New(ro bool, attrs []string) *Batch {
	return &Batch{
		Ro:    ro,
		Attrs: attrs,
		Vecs:  make([]*vector.Vector, len(attrs)),
	}
}

func (bat *Batch) Reorder(attrs []string) {
	if bat.Ro {
		bat.Cow()
	}
	for i, name := range attrs {
		for j, attr := range bat.Attrs {
			if name == attr {
				if len(bat.Is) > j {
					bat.Is[i], bat.Is[j] = bat.Is[j], bat.Is[i]
				}
				bat.Vecs[i], bat.Vecs[j] = bat.Vecs[j], bat.Vecs[i]
				bat.Attrs[i], bat.Attrs[j] = bat.Attrs[j], bat.Attrs[i]
			}
		}
	}
}

func (bat *Batch) Shuffle(proc *process.Process) {
	if bat.SelsData != nil {
		for i, vec := range bat.Vecs {
			bat.Vecs[i] = vec.Shuffle(bat.Sels)
		}
		proc.Free(bat.SelsData)
		bat.Sels = nil
		bat.SelsData = nil
	}
}

func (bat *Batch) Length(proc *process.Process) int {
	return bat.Vecs[0].Length()
}

func (bat *Batch) Prefetch(attrs []string, vecs []*vector.Vector, proc *process.Process) {
	for i, attr := range attrs {
		vecs[i] = bat.GetVector(attr, proc)
	}
}

func (bat *Batch) GetVector(name string, proc *process.Process) *vector.Vector {
	for i, attr := range bat.Attrs {
		if attr != name {
			continue
		}
		return bat.Vecs[i]
	}
	return nil
}

func (bat *Batch) Clean(proc *process.Process) {
	if bat.SelsData != nil {
		proc.Free(bat.SelsData)
		bat.Sels = nil
		bat.SelsData = nil
	}
	for _, vec := range bat.Vecs {
		if vec != nil {
			vec.Clean(proc)
		}
	}
}

func (bat *Batch) Reduce(attrs []string, proc *process.Process) {
	if bat.Ro {
		bat.Cow()
	}
	for _, attr := range attrs {
		for i := 0; i < len(bat.Attrs); i++ {
			if bat.Attrs[i] != attr {
				continue
			}
			bat.Vecs[i].Free(proc)
			if bat.Vecs[i].Data == nil {
				if len(bat.Is) > i {
					bat.Is = append(bat.Is[:i], bat.Is[i+1:]...)
				}
				bat.Vecs = append(bat.Vecs[:i], bat.Vecs[i+1:]...)
				bat.Attrs = append(bat.Attrs[:i], bat.Attrs[i+1:]...)
				i--
			}
			break
		}
	}
}

func (bat *Batch) Cow() {
	attrs := make([]string, len(bat.Attrs))
	for i, attr := range bat.Attrs {
		attrs[i] = attr
	}
	bat.Ro = false
	bat.Attrs = attrs
}

func (bat *Batch) String() string {
	var buf bytes.Buffer

	if len(bat.Sels) > 0 {
		fmt.Printf("%v\n", bat.Sels)
	}
	for i, attr := range bat.Attrs {
		buf.WriteString(fmt.Sprintf("%s\n", attr))
		buf.WriteString(fmt.Sprintf("\t%s\n", bat.Vecs[i]))
	}
	return buf.String()
}
