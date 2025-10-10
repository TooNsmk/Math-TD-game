package main

import (
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"strconv"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text"
	"golang.org/x/image/font/basicfont"
)

const (
	ScreenW = 800
	ScreenH = 600
)

type Vec struct{ X, Y float64 }

type Enemy struct {
	HP    float64
	MaxHP float64
	Speed float64 // px/sec
	T     float64 // progress along path
}

type Tower struct {
	X, Y   float64
	Range  float64
	Damage float64
	Fire   float64 // ms
	Cd     float64
}

type Bullet struct {
	X, Y   float64
	Tx, Ty float64
	Speed  float64
	Damage float64
}

type Question struct {
	Text string
	Ans  int
}

type Game struct {
	path    []Vec
	enemies []*Enemy
	towers  []*Tower
	bullets []*Bullet

	lastSpawn float64
	spawnInt  float64

	selected  int
	lastClick Vec

	challengeActive bool
	question        *Question
	inputBuf        string

	rand *rand.Rand
}

func NewGame() *Game {
	g := &Game{
		path:     []Vec{{0, 300}, {200, 300}, {200, 100}, {600, 100}, {600, 400}, {800, 400}},
		spawnInt: 2000,
		selected: -1,
		rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	// starter tower
	g.towers = append(g.towers, &Tower{X: 150, Y: 220, Range: 120, Damage: 2, Fire: 700, Cd: 0})
	return g
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) { return ScreenW, ScreenH }

func (g *Game) Update() error {
	dt := 1.0 / 60.0 * 1000.0 // ms per frame approx

	// input: mouse just released
	if inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft) {
		x, y := ebiten.CursorPosition()
		gx := float64(x)
		gy := float64(y)
		// select near tower
		sel := -1
		for i, tw := range g.towers {
			if math.Hypot(tw.X-gx, tw.Y-gy) < 18 {
				sel = i
				break
			}
		}
		if sel >= 0 {
			g.selected = sel
		} else {
			g.selected = -1
			g.lastClick = Vec{gx, gy}
		}
	}

	// toggle challenge with C key
	if inpututil.IsKeyJustPressed(ebiten.KeyC) && !g.challengeActive {
		q := genQuestion(g.rand)
		g.question = q
		g.inputBuf = ""
		g.challengeActive = true
	}

	// while challenge active, capture numeric keys, backspace and enter
	if g.challengeActive {
		// digits
		digits := []ebiten.Key{ebiten.Key0, ebiten.Key1, ebiten.Key2, ebiten.Key3, ebiten.Key4, ebiten.Key5, ebiten.Key6, ebiten.Key7, ebiten.Key8, ebiten.Key9}
		for k, d := range digits {
			if inpututil.IsKeyJustPressed(d) {
				g.inputBuf += strconv.Itoa(k)
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) {
			if len(g.inputBuf) > 0 {
				g.inputBuf = g.inputBuf[:len(g.inputBuf)-1]
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyMinus) {
			if len(g.inputBuf) == 0 {
				g.inputBuf = "-"
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyKPEnter) {
			// submit
			ans, err := strconv.Atoi(g.inputBuf)
			if err == nil && ans == g.question.Ans {
				g.applyReward()
			}
			g.challengeActive = false
			g.inputBuf = ""
		}
		// also allow closing with Escape
		if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			g.challengeActive = false
			g.inputBuf = ""
		}
	}

	// spawn
	g.lastSpawn += dt
	if g.lastSpawn > g.spawnInt {
		g.spawnEnemy()
		g.lastSpawn = 0
	}

	// update enemies
	for i := len(g.enemies) - 1; i >= 0; i-- {
		e := g.enemies[i]
		seg := int(math.Floor(e.T))
		segLen := 1.0
		if seg < len(g.path)-1 {
			segLen = dist(g.path[seg], g.path[seg+1])
		}
		frac := (e.Speed * dt / 1000.0) / (segLen)
		e.T += frac
		if e.T >= float64(len(g.path)-1) {
			// reached end
			// remove
			g.enemies = append(g.enemies[:i], g.enemies[i+1:]...)
			continue
		}
	}

	// towers shooting
	for _, tw := range g.towers {
		tw.Cd -= dt
		if tw.Cd <= 0 {
			// find nearest target
			var target *Enemy
			best := 1e9
			for _, e := range g.enemies {
				p := g.posAlongPath(e.T)
				d := math.Hypot(p.X-tw.X, p.Y-tw.Y)
				if d <= tw.Range && d < best {
					best = d
					target = e
				}
			}
			if target != nil {
				p := g.posAlongPath(target.T)
				// fire
				tw.Cd = tw.Fire
				g.bullets = append(g.bullets, &Bullet{X: tw.X, Y: tw.Y, Tx: p.X, Ty: p.Y, Speed: 400, Damage: tw.Damage})
			}
		}
	}

	// bullets
	for i := len(g.bullets) - 1; i >= 0; i-- {
		b := g.bullets[i]
		dx := b.Tx - b.X
		dy := b.Ty - b.Y
		d := math.Hypot(dx, dy)
		move := b.Speed * dt / 1000.0
		if d <= move || d == 0 {
			// apply to nearest enemy at that position (simple)
			for j := range g.enemies {
				p := g.posAlongPath(g.enemies[j].T)
				if math.Hypot(p.X-b.Tx, p.Y-b.Ty) < 18 {
					g.enemies[j].HP -= b.Damage
					break
				}
			}
			g.bullets = append(g.bullets[:i], g.bullets[i+1:]...)
			continue
		}
		b.X += dx / d * move
		b.Y += dy / d * move
	}

	// remove dead enemies
	for i := len(g.enemies) - 1; i >= 0; i-- {
		if g.enemies[i].HP <= 0 {
			g.enemies = append(g.enemies[:i], g.enemies[i+1:]...)
		}
	}

	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	// clear
	screen.Fill(color.RGBA{0xA7, 0xD0, 0xFF, 0xFF})

	// draw path
	for i := 0; i < len(g.path)-1; i++ {
		p := g.path[i]
		n := g.path[i+1]
		ebitenutilDrawLine(screen, p.X, p.Y, n.X, n.Y, color.RGBA{0x33, 0x33, 0x33, 0xFF})
	}

	// enemies
	for _, e := range g.enemies {
		p := g.posAlongPath(e.T)
		ebitenutilFillCircle(screen, p.X, p.Y, 12, color.RGBA{0xD9, 0x53, 0x4F, 0xFF})
		// hp bar
		barW := 30.0
		healthW := barW * (e.HP / e.MaxHP)
		rect(screen, p.X-barW/2, p.Y-20, barW, 5, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF})
		rect(screen, p.X-barW/2, p.Y-20, healthW, 5, color.RGBA{0x5C, 0xB8, 0x5C, 0xFF})
	}

	// towers
	for i, tw := range g.towers {
		c := color.RGBA{0x2B, 0x6C, 0xB0, 0xFF}
		if g.selected == i {
			c = color.RGBA{0xFF, 0xCC, 0x00, 0xFF}
		}
		ebitenutilFillCircle(screen, tw.X, tw.Y, 14, c)
		// range
		rangec := color.RGBA{0x2B, 0x6C, 0xB0, 0x20}
		circleFill(screen, tw.X, tw.Y, tw.Range, rangec)
	}

	// bullets
	for _, b := range g.bullets {
		ebitenutilFillCircle(screen, b.X, b.Y, 4, color.RGBA{0x22, 0x22, 0x22, 0xFF})
	}

	// UI text
	text.Draw(screen, "Press C to open math challenge", basicfont.Face7x13, 10, 20, color.White)
	if g.selected >= 0 {
		tw := g.towers[g.selected]
		text.Draw(screen, fmt.Sprintf("Selected Tower: dmg=%.0f range=%.0f fire=%.0fms", tw.Damage, tw.Range, tw.Fire), basicfont.Face7x13, 10, 40, color.White)
	}
	text.Draw(screen, "Click to select a tower or set place point. Press C for challenge.", basicfont.Face7x13, 10, 60, color.White)

	// last click indicator
	if g.selected == -1 {
		text.Draw(screen, fmt.Sprintf("Placement point: %.0f, %.0f (click then press C)", g.lastClick.X, g.lastClick.Y), basicfont.Face7x13, 10, 80, color.White)
	}

	// challenge overlay
	if g.challengeActive && g.question != nil {
		// translucent box
		w := 500.0
		h := 140.0
		rect(screen, (ScreenW-w)/2, (ScreenH-h)/2, w, h, color.RGBA{0, 0, 0, 0x80})
		text.Draw(screen, "Solve:", basicfont.Face7x13, int((ScreenW-w)/2+20), int((ScreenH-h)/2+30), color.White)
		text.Draw(screen, g.question.Text, basicfont.Face7x13, int((ScreenW-w)/2+20), int((ScreenH-h)/2+60), color.White)
		text.Draw(screen, "Answer: "+g.inputBuf, basicfont.Face7x13, int((ScreenW-w)/2+20), int((ScreenH-h)/2+90), color.White)
		text.Draw(screen, "Enter to submit, Esc to cancel", basicfont.Face7x13, int((ScreenW-w)/2+20), int((ScreenH-h)/2+120), color.White)
	}
}

func (g *Game) spawnEnemy() {
	e := &Enemy{HP: 5 + float64(g.rand.Intn(6)), MaxHP: 5, Speed: 40 + g.rand.Float64()*40, T: 0}
	g.enemies = append(g.enemies, e)
}

func (g *Game) posAlongPath(t float64) Vec {
	i := int(math.Floor(t))
	frac := t - float64(i)
	if i >= len(g.path)-1 {
		p := g.path[len(g.path)-1]
		return p
	}
	a := g.path[i]
	b := g.path[i+1]
	return Vec{a.X + (b.X-a.X)*frac, a.Y + (b.Y-a.Y)*frac}
}

func (g *Game) applyReward() {
	reward := g.rand.Float64()
	if g.selected >= 0 {
		tw := g.towers[g.selected]
		if reward < 0.33 {
			tw.Damage += 1
		} else if reward < 0.66 {
			tw.Range += 20
		} else {
			tw.Fire = math.Max(150, tw.Fire-100)
		}
	} else {
		pos := g.lastClick
		if pos.X == 0 && pos.Y == 0 {
			pos = Vec{100, 250}
		}
		g.towers = append(g.towers, &Tower{X: pos.X, Y: pos.Y, Range: 120, Damage: 2, Fire: 700, Cd: 0})
	}
}

func genQuestion(r *rand.Rand) *Question {
	a := 1 + r.Intn(12)
	b := 1 + r.Intn(12)
	opi := r.Intn(3)
	var op string
	var ans int
	if opi == 0 {
		op = "+"
		ans = a + b
	} else if opi == 1 {
		op = "-"
		ans = a - b
	} else {
		op = "*"
		ans = a * b
	}
	return &Question{Text: fmt.Sprintf("%d %s %d", a, op, b), Ans: ans}
}

// --- minimal drawing helpers (avoid additional deps) ---

func rect(img *ebiten.Image, x, y, w, h float64, c color.Color) {
	r := ebiten.NewImage(int(w), int(h))
	r.Fill(c)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(x, y)
	img.DrawImage(r, op)
}

func circleFill(img *ebiten.Image, cx, cy, r float64, c color.Color) {
	// crude: draw many small rects along radial steps
	steps := int(math.Max(8, r/2))
	for i := 0; i < steps; i++ {
		ang := 2 * math.Pi * float64(i) / float64(steps)
		x := cx + math.Cos(ang)*r
		y := cy + math.Sin(ang)*r
		rect(img, x-1, y-1, 2, 2, c)
	}
}

// tiny util functions to avoid ebitenutil dependency for lines/circles
func ebitenutilDrawLine(img *ebiten.Image, x1, y1, x2, y2 float64, c color.Color) {
	// draw a thin rectangle approximating a line
	dx := x2 - x1
	dy := y2 - y1
	len := math.Hypot(dx, dy)
	if len == 0 {
		return
	}
	ang := math.Atan2(dy, dx)
	line := ebiten.NewImage(int(len), 6)
	line.Fill(c)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(int(len))/2, -3)
	op.GeoM.Rotate(ang)
	op.GeoM.Translate((x1+x2)/2, (y1+y2)/2)
	img.DrawImage(line, op)
}

func ebitenutilFillCircle(img *ebiten.Image, cx, cy, r float64, c color.Color) {
	// draw simple filled circle using many rects
	steps := int(math.Max(12, r))
	for i := 0; i < steps; i++ {
		ang := 2 * math.Pi * float64(i) / float64(steps)
		x := cx + math.Cos(ang)*r
		y := cy + math.Sin(ang)*r
		rect(img, x-2, y-2, 4, 4, c)
	}
}

func dist(a, b Vec) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }

func main() {
	g := NewGame()
	ebiten.SetWindowSize(ScreenW, ScreenH)
	ebiten.SetWindowTitle("DataGame — Math Tower Defense (Go/Ebiten)")
	if err := ebiten.RunGame(g); err != nil {
		panic(err)
	}
}
