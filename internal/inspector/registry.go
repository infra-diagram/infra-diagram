package inspector

import "sort"

// Registry es el catálogo de insights disponibles.
//
// Thread-safe para reads (List, Get) asumiendo Register se llama solo durante
// la inicialización (single-threaded). Si en el futuro hay registro dinámico,
// agregamos un mutex.
type Registry struct {
	insights map[string]Insight
}

func NewRegistry() *Registry {
	return &Registry{insights: make(map[string]Insight)}
}

// Register agrega un insight al catálogo. Si ya existe uno con el mismo ID,
// lo sobreescribe (útil para tests).
func (r *Registry) Register(i Insight) {
	r.insights[i.ID()] = i
}

// Get retorna un insight por ID, o (nil, false) si no existe.
func (r *Registry) Get(id string) (Insight, bool) {
	i, ok := r.insights[id]
	return i, ok
}

// List retorna todos los insights registrados, ordenados por ID para
// consistencia en outputs.
func (r *Registry) List() []Insight {
	out := make([]Insight, 0, len(r.insights))
	for _, i := range r.insights {
		out = append(out, i)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID() < out[j].ID()
	})
	return out
}
