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
	Armor float64
	Speed float64 // px/sec
	T     float64 // progress along path
	// status effects
	BurnTime   float64 // ms remaining
	BurnLevel  int     // damage multiplier level for burn
	BurnTick   float64 // accumulator for burn tick interval (ms)
	SlowTime   float64 // ms remaining for slow
	SlowFactor float64 // multiplier applied to speed when slowed (0-1)
}

type Tower struct {
	X, Y   float64
	Range  float64
	Damage float64
	Fire   float64 // ms
	Cd     float64
	Type   string // "normal", "flame", "slow"
	// optional for special towers
	FlameDuration float64 // ms that a flame effect lasts on target when hit
	PulseDuration float64 // ms that a slow pulse lasts on enemy
}

type Bullet struct {
	X, Y        float64
	Tx, Ty      float64
	Speed       float64
	Damage      float64
	Penetration float64
	AoeRadius   float64
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
	// level progression
	killCount          int
	nextLevelThreshold int
	level              int
	levelMsg           string
	levelMsgTimer      float64 // ms
	// player stats
	playerHP    float64
	playerArmor float64
	playerGold  int
	// shop / upgrades
	shopActive bool
	// upgrade levels
	upDamageLevel int
	upSpeedLevel  int
	upPenLevel    int
	upAOELevel    int
}

func NewGame() *Game {
	g := &Game{
		path:     []Vec{{0, 300}, {200, 300}, {200, 100}, {600, 100}, {600, 400}, {800, 400}},
		spawnInt: 2000,
		selected: -1,
		rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	// starter tower
	g.towers = append(g.towers, &Tower{X: 150, Y: 220, Range: 120, Damage: 2, Fire: 700, Cd: 0, Type: "normal"})
	// flame tower
	g.towers = append(g.towers, &Tower{X: 300, Y: 220, Range: 100, Damage: 0, Fire: 200, Cd: 0, Type: "flame", FlameDuration: 5000})
	// slowing tower (pulse)
	g.towers = append(g.towers, &Tower{X: 450, Y: 220, Range: 140, Damage: 0, Fire: 1500, Cd: 0, Type: "slow", PulseDuration: 1200})
	// initial level threshold
	g.nextLevelThreshold = 20 + g.rand.Intn(11) // 20..30
	g.level = 1
	// player defaults
	g.playerHP = 100.0
	g.playerArmor = 2.0
	g.playerGold = 0
	// upgrades
	g.shopActive = false
	g.upDamageLevel = 0
	g.upSpeedLevel = 0
	g.upPenLevel = 0
	g.upAOELevel = 0
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
		// if shop active, handle purchase clicks
		if g.shopActive {
			g.handleShopClick(gx, gy)
		}
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

	// toggle shop with B key
	if inpututil.IsKeyJustPressed(ebiten.KeyB) {
		g.shopActive = !g.shopActive
		// close challenge if shop opened
		if g.shopActive {
			g.challengeActive = false
		}
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
			// reached end -> enemy escaped: damage the player (armor mitigates flat damage)
			baseDmg := 10.0
			mitig := baseDmg - g.playerArmor
			if mitig < 1.0 {
				mitig = 1.0
			}
			g.playerHP -= mitig
			// remove enemy
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
				if tw.Type == "flame" {
					// flamethrower: apply burn status to target
					target.BurnTime = math.Max(target.BurnTime, tw.FlameDuration)
					// burn level scales with game level
					target.BurnLevel = g.level
					// also create short lived visual bullet for flame
					dmg := 100.0
					// damage multiplier from upgrades: 10% per level
					dmg *= 1.0 + 0.10*float64(g.upDamageLevel)
					pen := float64(g.upPenLevel)
					aoe := 0.0 + 4.0*float64(g.upAOELevel)
					g.bullets = append(g.bullets, &Bullet{X: tw.X, Y: tw.Y, Tx: p.X, Ty: p.Y, Speed: 800, Damage: dmg, Penetration: pen, AoeRadius: aoe})
				} else if tw.Type == "slow" {
					// apply slow pulse
					target.SlowTime = math.Max(target.SlowTime, tw.PulseDuration)
					// slow factor scales with tower damage field (if any), default 0.5
					target.SlowFactor = 0.5
					dmg := 100.0
					dmg *= 1.0 + 0.10*float64(g.upDamageLevel)
					pen := float64(g.upPenLevel)
					aoe := 0.0 + 4.0*float64(g.upAOELevel)
					g.bullets = append(g.bullets, &Bullet{X: tw.X, Y: tw.Y, Tx: p.X, Ty: p.Y, Speed: 600, Damage: dmg, Penetration: pen, AoeRadius: aoe})
				} else {
					// base damage adjusted by tower damage and upgrades
					base := tw.Damage
					base *= 1.0 + 0.10*float64(g.upDamageLevel)
					// fire rate speedup: each speed level reduces Fire by 10%
					tw.Fire = tw.Fire * math.Pow(0.90, float64(g.upSpeedLevel))
					pen := float64(g.upPenLevel)
					aoe := 0.0 + 4.0*float64(g.upAOELevel)
					g.bullets = append(g.bullets, &Bullet{X: tw.X, Y: tw.Y, Tx: p.X, Ty: p.Y, Speed: 400, Damage: base, Penetration: pen, AoeRadius: aoe})
				}
			}
		}
	}

	// process enemy status effects (burn damage over time, slow timers)
	for _, e := range g.enemies {
		// burn: deal damage per tick (1000ms tick) scaled by level
		if e.BurnTime > 0 {
			e.BurnTick += dt
			for e.BurnTick >= 1000 {
				// each tick deals 10 damage * level
				dmg := float64(100 * e.BurnLevel)
				e.HP -= dmg
				e.BurnTick -= 1000
			}
			e.BurnTime -= dt
			if e.BurnTime < 0 {
				e.BurnTime = 0
			}
		}
		// slow: decrement timer
		if e.SlowTime > 0 {
			e.SlowTime -= dt
			if e.SlowTime < 0 {
				e.SlowTime = 0
				e.SlowFactor = 1.0
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
			// apply damage at impact point, considering penetration and AoE
			g.applyDamageAt(b.Tx, b.Ty, b.Damage, b.Penetration, b.AoeRadius)
			g.bullets = append(g.bullets[:i], g.bullets[i+1:]...)
			continue
		}
		b.X += dx / d * move
		b.Y += dy / d * move
	}

	// remove dead enemies
	for i := len(g.enemies) - 1; i >= 0; i-- {
		if g.enemies[i].HP <= 0 {
			// count kills
			g.killCount++
			// award gold: multiples of 10. Use current killCount as multiplier (e.g., 1st kill = 10, 2nd = 20...)
			goldAward := 10 * g.killCount
			g.playerGold += goldAward
			// remove
			g.enemies = append(g.enemies[:i], g.enemies[i+1:]...)
			// check for new level
			if g.killCount >= g.nextLevelThreshold {
				g.newLevel()
			}
		}
	}

	// decrement level message timer
	if g.levelMsgTimer > 0 {
		g.levelMsgTimer -= dt
		if g.levelMsgTimer < 0 {
			g.levelMsgTimer = 0
			g.levelMsg = ""
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
		// visual tinting: burning -> reddish, slowed -> bluish
		col := color.RGBA{0xD9, 0x53, 0x4F, 0xFF}
		if e.BurnTime > 0 {
			// stronger red when burn active
			col = color.RGBA{0xFF, 0x88, 0x66, 0xFF}
		}
		if e.SlowTime > 0 {
			// mix with blue tint when slowed
			col = color.RGBA{0x66, 0x99, 0xFF, 0xFF}
		}
		ebitenutilFillCircle(screen, p.X, p.Y, 12, col)

		// flame particles for burning enemies
		if e.BurnTime > 0 {
			// draw a few small flicker rects above the enemy
			for i := 0; i < 6; i++ {
				offx := (float64(i)-3.0)*2.0 + math.Sin(float64(i)+e.BurnTick/50.0)*2.0
				offy := -6.0 + math.Mod(e.BurnTick/100.0, 6.0)
				rect(screen, p.X+offx, p.Y+offy, 3, 3, color.RGBA{0xFF, 0x66, 0x00, 0xFF})
			}
		}

		// slow ring indicator
		if e.SlowTime > 0 {
			ringR := 18.0 + (e.SlowTime/1000.0)*6.0
			rect(screen, p.X-ringR/2, p.Y-ringR/2, ringR, 2, color.RGBA{0x66, 0x99, 0xFF, 0x80})
		}
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
	drawText(screen, "Press C to open math challenge", 10, 20, color.White)
	// player stats
	drawText(screen, fmt.Sprintf("HP: %.0f", g.playerHP), ScreenW-180, 20, color.White)
	drawText(screen, fmt.Sprintf("Armor: %.0f", g.playerArmor), ScreenW-180, 40, color.White)
	drawText(screen, fmt.Sprintf("Gold: %d", g.playerGold), ScreenW-180, 60, color.White)
	if g.selected >= 0 {
		tw := g.towers[g.selected]
		drawText(screen, fmt.Sprintf("Selected Tower: dmg=%.0f range=%.0f fire=%.0fms", tw.Damage, tw.Range, tw.Fire), 10, 40, color.White)
	}
	drawText(screen, "Click to select a tower or set place point. Press C for challenge.", 10, 60, color.White)

	// last click indicator
	if g.selected == -1 {
		drawText(screen, fmt.Sprintf("Placement point: %.0f, %.0f (click then press C)", g.lastClick.X, g.lastClick.Y), 10, 80, color.White)
	}

	// challenge overlay
	if g.challengeActive && g.question != nil {
		// translucent box
		w := 500.0
		h := 140.0
		rect(screen, (ScreenW-w)/2, (ScreenH-h)/2, w, h, color.RGBA{0, 0, 0, 0x80})
		drawText(screen, "Solve:", int((ScreenW-w)/2+20), int((ScreenH-h)/2+30), color.White)
		drawText(screen, g.question.Text, int((ScreenW-w)/2+20), int((ScreenH-h)/2+60), color.White)
		drawText(screen, "Answer: "+g.inputBuf, int((ScreenW-w)/2+20), int((ScreenH-h)/2+90), color.White)
		drawText(screen, "Enter to submit, Esc to cancel", int((ScreenW-w)/2+20), int((ScreenH-h)/2+120), color.White)
	}

	// shop overlay
	if g.shopActive {
		w := 420.0
		h := 260.0
		x0 := (ScreenW - int(w)) / 2
		y0 := (ScreenH - int(h)) / 2
		rect(screen, float64(x0), float64(y0), w, h, color.RGBA{0, 0, 0, 0xC0})
		drawText(screen, "Shop - Buy Upgrades (press B to close)", x0+10, y0+20, color.White)
		drawText(screen, fmt.Sprintf("Gold: %d", g.playerGold), x0+300, y0+20, color.White)

		// each upgrade line: label (x,y) and cost and level
		lines := []struct {
			label string
			level int
			cost  int
		}{
			{"Damage +10%", g.upDamageLevel, 50 * (1 + g.upDamageLevel)},
			{"Fire Rate +10%", g.upSpeedLevel, 40 * (1 + g.upSpeedLevel)},
			{"Armor Penetration +1", g.upPenLevel, 60 * (1 + g.upPenLevel)},
			{"AOE Radius +4px", g.upAOELevel, 80 * (1 + g.upAOELevel)},
		}
		for i, l := range lines {
			yy := y0 + 50 + i*40
			drawText(screen, fmt.Sprintf("%s (Lv %d) - Cost: %d", l.label, l.level, l.cost), x0+10, yy, color.White)
			drawText(screen, "Click to buy", x0+300, yy, color.White)
		}
	}

	// level message
	if g.levelMsgTimer > 0 && g.levelMsg != "" {
		drawText(screen, g.levelMsg, 10, ScreenH-20, color.White)
	}
}

// drawText is a small wrapper that uses the classic text.Draw signature
func drawText(img *ebiten.Image, s string, x, y int, col color.Color) {
	text.Draw(img, s, basicfont.Face7x13, x, y, col)
}

func (g *Game) spawnEnemy() {
	// scale HP between 100 and 1000 based on level randomness
	hp := 100.0 + g.rand.Float64()*(1000.0-100.0)
	// give enemies a small armor that scales with level
	armor := float64(g.level) * 0.5
	e := &Enemy{HP: hp, MaxHP: hp, Armor: armor, Speed: 40 + g.rand.Float64()*40, T: 0}
	g.enemies = append(g.enemies, e)
}

// handleShopClick checks if the click was on a shop button and purchases if affordable
func (g *Game) handleShopClick(x, y float64) {
	w := 420.0
	h := 260.0
	x0 := float64((ScreenW - int(w)) / 2)
	y0 := float64((ScreenH - int(h)) / 2)
	if x < x0 || x > x0+w || y < y0 || y > y0+h {
		return
	}
	// compute which line clicked
	relY := int(y - (y0 + 50))
	if relY < 0 || relY > 200 {
		return
	}
	idx := relY / 40
	switch idx {
	case 0:
		cost := 50 * (1 + g.upDamageLevel)
		if g.playerGold >= cost {
			g.playerGold -= cost
			g.upDamageLevel++
		}
	case 1:
		cost := 40 * (1 + g.upSpeedLevel)
		if g.playerGold >= cost {
			g.playerGold -= cost
			g.upSpeedLevel++
		}
	case 2:
		cost := 60 * (1 + g.upPenLevel)
		if g.playerGold >= cost {
			g.playerGold -= cost
			g.upPenLevel++
		}
	case 3:
		cost := 80 * (1 + g.upAOELevel)
		if g.playerGold >= cost {
			g.playerGold -= cost
			g.upAOELevel++
		}
	}
}

// applyDamageAt applies damage to an enemy index or AoE around a point, considering penetration and enemy armor
func (g *Game) applyDamageAt(x, y, baseDamage float64, penetration float64, aoeRadius float64) {
	if aoeRadius <= 0 {
		// find nearest enemy at point
		best := -1
		bestD := 1e9
		for i, e := range g.enemies {
			p := g.posAlongPath(e.T)
			d := math.Hypot(p.X-x, p.Y-y)
			if d < bestD {
				bestD = d
				best = i
			}
		}
		if best >= 0 && bestD < 18 {
			e := g.enemies[best]
			// effective armor after penetration
			effArmor := math.Max(0, e.Armor-penetration)
			dmg := baseDamage - effArmor
			if dmg < 1 {
				dmg = 1
			}
			e.HP -= dmg
		}
		return
	}
	// AoE: damage all enemies within radius
	for _, e := range g.enemies {
		p := g.posAlongPath(e.T)
		if math.Hypot(p.X-x, p.Y-y) <= aoeRadius {
			effArmor := math.Max(0, e.Armor-penetration)
			dmg := baseDamage - effArmor
			if dmg < 1 {
				dmg = 1
			}
			e.HP -= dmg
		}
	}
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

func (g *Game) newLevel() {
	g.level++
	g.killCount = 0
	g.nextLevelThreshold = 20 + g.rand.Intn(11)
	// generate a new random path with 5-7 waypoints across the screen
	wp := 3 + g.rand.Intn(5) // 3..7 segments
	newPath := make([]Vec, 0, wp+2)
	// start at left edge
	newPath = append(newPath, Vec{0, 300})
	for i := 0; i < wp; i++ {
		x := float64(100 + g.rand.Intn(ScreenW-200))
		y := float64(80 + g.rand.Intn(ScreenH-160))
		newPath = append(newPath, Vec{x, y})
	}
	// end at right edge
	newPath = append(newPath, Vec{ScreenW, 300})
	g.path = newPath
	// reduce spawn interval slightly to increase challenge
	if g.spawnInt > 600 {
		g.spawnInt -= 150
	}
	// set a temporary level message
	g.levelMsg = fmt.Sprintf("Level %d - New path generated! Next threshold: %d kills", g.level, g.nextLevelThreshold)
	g.levelMsgTimer = 3000 // show for 3s
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
