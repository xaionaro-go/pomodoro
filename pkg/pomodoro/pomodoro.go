package pomodoro

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"log"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ebitengine/oto/v3"
	"github.com/jfreymuth/oggvorbis"
)

const (
	audioEnabled = false
)

type Pomodoro struct {
	fyne.App
	fyne.Window
	Description      *canvas.Text
	MinutesText      *canvas.Text
	Delimiter        *canvas.Text
	SecondsText      *canvas.Text
	Deadline         time.Time
	NextWorkInterval time.Duration
	NextRestInterval time.Duration
	IsWork           bool

	Locker       sync.Mutex
	TickerCancel context.CancelFunc
}

func New() *Pomodoro {
	a := app.NewWithID("center.dx.fynodoro")
	w := a.NewWindow("Pomodoro (DX)")
	w.CenterOnScreen()
	w.SetMaster()
	p := &Pomodoro{
		App:              a,
		Window:           w,
		IsWork:           true,
		NextRestInterval: 15 * time.Minute,
	}
	textStyle := fyne.TextStyle{Monospace: true}
	p.Description = canvas.NewText("", color.Gray{Y: 224})
	p.Description.Alignment = fyne.TextAlignCenter
	p.Description.TextSize = 45
	p.Description.TextStyle = textStyle
	descriptionContainer := container.NewHBox(p.Description)
	p.MinutesText = canvas.NewText("", color.White)
	p.MinutesText.TextSize = 90
	p.MinutesText.TextStyle = textStyle
	p.Delimiter = canvas.NewText(":", color.Gray{Y: 128})
	p.Delimiter.TextSize = 90
	p.Delimiter.TextStyle = textStyle
	p.SecondsText = canvas.NewText("", color.White)
	p.SecondsText.TextSize = 90
	p.SecondsText.TextStyle = textStyle
	timerContainer := container.NewStack(container.NewHBox(
		p.MinutesText,
		p.Delimiter,
		p.SecondsText,
	))
	set5MinsButton := widget.NewButton("  5  ", func() { p.SetNextInterval(5 * time.Minute) })
	set15MinsButton := widget.NewButton(" 15 ", func() { p.SetNextInterval(15 * time.Minute) })
	set30MinsButton := widget.NewButton(" 30 ", func() { p.SetNextInterval(30 * time.Minute) })
	set45MinsButton := widget.NewButton(" 45 ", func() { p.SetNextInterval(45 * time.Minute) })
	set60MinsButton := widget.NewButton(" 60 ", func() { p.SetNextInterval(60 * time.Minute) })
	set75MinsButton := widget.NewButton(" 75 ", func() { p.SetNextInterval(75 * time.Minute) })
	set90MinsButton := widget.NewButton(" 90 ", func() { p.SetNextInterval(90 * time.Minute) })
	set105MinsButton := widget.NewButton("105", func() { p.SetNextInterval(105 * time.Minute) })
	setIsWorkButton := widget.NewButtonWithIcon("WORK", theme.MediaPlayIcon(), func() { p.Start(true) })
	setIsRestButton := widget.NewButtonWithIcon("REST", theme.MediaPlayIcon(), func() { p.Start(false) })
	stopButton := widget.NewButtonWithIcon("STOP", theme.MediaStopIcon(), p.StopTimer)
	controlsLine0Container := container.NewHBox(
		set5MinsButton,
		set15MinsButton,
		set30MinsButton,
		set45MinsButton,
		setIsWorkButton,
	)
	controlsLine1Container := container.NewHBox(
		set60MinsButton,
		set75MinsButton,
		set90MinsButton,
		set105MinsButton,
		setIsRestButton,
		stopButton,
	)
	w.Canvas().SetContent(container.NewVBox(
		descriptionContainer,
		timerContainer,
		controlsLine0Container,
		controlsLine1Container,
	))
	p.SetNextInterval(60 * time.Minute)
	return p
}

func (p *Pomodoro) SetNextInterval(
	nextInterval time.Duration,
) {
	p.Locker.Lock()
	defer p.Locker.Unlock()
	if p.IsWork {
		p.NextWorkInterval = nextInterval
	} else {
		p.NextRestInterval = nextInterval
	}
	p.Deadline = time.Now().Add(nextInterval)
	p.setTimeLeft(nextInterval)
}

func (p *Pomodoro) SetTimeLeft(
	timeLeft time.Duration,
) {
	p.Locker.Lock()
	defer p.Locker.Unlock()
	p.setTimeLeft(timeLeft)
}

func (p *Pomodoro) setTimeLeft(
	timeLeft time.Duration,
) {
	timeLeft += 200 * time.Millisecond
	minutes := uint(timeLeft / time.Minute)
	seconds := uint((timeLeft % time.Minute) / time.Second)
	p.MinutesText.Text = fmt.Sprintf("%2d", minutes)
	p.SecondsText.Text = fmt.Sprintf("%02d", seconds)
	p.MinutesText.Refresh()
	p.SecondsText.Refresh()
}

func (p *Pomodoro) Start(
	isWork bool,
) {
	p.Locker.Lock()
	p.setIsWork(isWork)

	ctx, cancelFn := context.WithCancel(context.Background())
	if p.TickerCancel != nil {
		p.TickerCancel()
	}
	p.TickerCancel = cancelFn
	if p.IsWork {
		p.Deadline = time.Now().Add(p.NextWorkInterval)
	} else {
		p.Deadline = time.Now().Add(p.NextRestInterval)
	}
	p.Locker.Unlock()

	ticker := time.NewTicker(time.Second)
	go func() {
		defer func() {
			ticker.Stop()
			ticker = nil
		}()
		p.Tick()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			p.Tick()
		}
	}()
}

func (p *Pomodoro) StopTimer() {
	p.Locker.Lock()
	defer p.Locker.Unlock()
	p.Description.Text = ""
	if p.TickerCancel != nil {
		p.TickerCancel()
	}
	p.TickerCancel = nil
	p.Delimiter.Color = color.Gray{Y: 128}
	p.Delimiter.Refresh()
}

func (p *Pomodoro) Tick() {
	p.Locker.Lock()
	defer p.Locker.Unlock()
	if r, _, _, _ := p.Delimiter.Color.RGBA(); r > 10000 {
		p.Delimiter.Color = color.Gray{Y: 22}
	} else {
		p.Delimiter.Color = color.Gray{Y: 128}
	}
	p.Delimiter.Refresh()

	timeLeft := time.Until(p.Deadline)
	if timeLeft <= 0 {
		p.endTimer()
		return
	}
	p.setTimeLeft(timeLeft)
}

func (p *Pomodoro) EndTimer() {
	p.Locker.Lock()
	defer p.Locker.Unlock()
	p.endTimer()
}

func (p *Pomodoro) setIsWork(isWork bool) {
	if isWork {
		p.Description.Text = "FOCUS"
		p.setTimeLeft(p.NextWorkInterval)
	} else {
		p.Description.Text = "REST"
		p.setTimeLeft(p.NextRestInterval)
	}
	p.IsWork = isWork
}

func (p *Pomodoro) endTimer() {
	if p.TickerCancel != nil {
		p.TickerCancel()
		p.TickerCancel = nil
	}
	if audioEnabled {
		go func() {
			err := p.playAlarm()
			if err != nil {
				log.Printf("%v", fmt.Errorf("unable to play the alarm sound: %w", err))
			}
		}()
	}
	p.setIsWork(!p.IsWork)
}

func (p *Pomodoro) playAlarm() error {
	oggDecoder, err := oggvorbis.NewReader(bytes.NewReader(alarmSoundFile))
	if err != nil {
		return fmt.Errorf("unable to initialize a decoder of the ogg vorbis audio: %w", err)
	}

	buffer := make([]float32, 671558)
	n, err := oggDecoder.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("unable to decode the ogg vorbis file: %w", err)
	}
	buffer = buffer[:n]

	op := &oto.NewContextOptions{
		SampleRate:   oggDecoder.SampleRate(),
		ChannelCount: oggDecoder.Channels(),
		Format:       oto.FormatFloat32LE,
		BufferSize:   0,
	}
	otoCtx, readyChan, err := oto.NewContext(op)
	if err != nil {
		return fmt.Errorf("unable to initialize an oto context: %w", err)
	}
	<-readyChan

	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&buffer))
	hdr.Cap *= 4
	hdr.Len *= 4

	player := otoCtx.NewPlayer(bytes.NewReader(*(*[]byte)(unsafe.Pointer(hdr))))
	player.Play()
	for player.IsPlaying() {
		time.Sleep(100 * time.Millisecond)
	}

	err = player.Close()
	if err != nil {
		return fmt.Errorf("unable to close the player: %w", err)
	}

	return nil
}
