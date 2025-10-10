DataGame — Math Tower Defense (Go / Ebiten)

This is a small prototype written in Go using Ebiten (2D game library).

Requirements
- Go 1.18+ installed

Run
From PowerShell:

```powershell
cd "C:\Users\End User\Desktop\datagame-go"
go mod tidy
go run .
```

Controls
- Left click: select tower (click near a tower) or set placement point (click empty space)
- C: open a math challenge. Type the answer using number keys (Backspace to edit). Press Enter to submit, Esc to cancel.
- Correct answer: upgrades selected tower or places a new tower at the last clicked location.

Next steps you might want
- Add money/score system and a shop
- Improve graphics and animations
- Add sound effects and more question difficulties
