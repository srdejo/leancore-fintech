// Package clock implementa ports.Clock con el reloj del sistema en una zona
// horaria dada (por defecto America/Bogota).
package clock

import (
	"time"
	_ "time/tzdata" // zonas horarias embebidas (imagen mínima sin tzdata)
)

type System struct{ loc *time.Location }

func NewSystem(zone string) (System, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return System{}, err
	}
	return System{loc: loc}, nil
}

// Today devuelve la fecha civil actual en la zona configurada, a medianoche UTC.
func (s System) Today() time.Time {
	y, m, d := time.Now().In(s.loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
