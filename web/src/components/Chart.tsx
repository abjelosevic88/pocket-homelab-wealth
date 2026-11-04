// Charts: a line over time (net worth, one asset, the plan) with a crosshair
// and a tooltip, and a list of bars (what makes up net worth). Drawn at the
// real pixel width, so nothing stretches; labels are HTML in text colours.
import { useEffect, useLayoutEffect, useRef, useState } from 'react'

export interface Series { key: string; label: string; values: number[]; dashed?: boolean; muted?: boolean }

function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const [w, setW] = useState(0)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    setW(el.clientWidth)
    const ro = new ResizeObserver(() => setW(el.clientWidth))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, w] as const
}

function niceStep(range: number, ticks: number) {
  const raw = range / Math.max(1, ticks)
  const p = Math.pow(10, Math.floor(Math.log10(raw || 1)))
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= raw) return m * p
  return 10 * p
}

/**
 * A line chart. labels[i] belongs to every series' values[i]; tip(i) is the
 * tooltip; ref is an optional horizontal reference line (a target).
 */
export function LineChart({ labels, series, height = 220, fmt, fmtAxis, tip, ariaLabel, refLine, area = true }: {
  labels: string[]; series: Series[]; height?: number; fmt: (v: number) => string; fmtAxis?: (v: number) => string
  tip: (i: number) => React.ReactNode; ariaLabel: string; refLine?: { value: number; label: string }; area?: boolean
}) {
  const [box, width] = useWidth<HTMLDivElement>()
  const [hover, setHover] = useState<number | null>(null)
  const n = labels.length
  const all = series.flatMap(s => s.values).concat(refLine ? [refLine.value] : [])
  let lo = Math.min(0, ...all), hi = Math.max(...all, 0)
  if (lo === hi) hi = lo + 1
  const step = niceStep(hi - lo, 4)
  lo = Math.floor(lo / step) * step
  hi = Math.ceil(hi / step) * step
  const ticks: number[] = []
  for (let v = lo; v <= hi + step / 2; v += step) ticks.push(v)
  const padL = width < 480 ? 50 : 62, padR = 6, padT = 10, padB = 4
  const w = Math.max(0, width - padL - padR), h = height - padT - padB
  const x = (i: number) => padL + (n <= 1 ? w / 2 : (i / (n - 1)) * w)
  const y = (v: number) => padT + h - ((v - lo) / (hi - lo)) * h
  const path = (vals: number[]) => vals.map((v, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join('')
  const pick = (clientX: number) => {
    const el = box.current
    if (!el || n === 0) return
    const r = el.getBoundingClientRect()
    const i = Math.round(((clientX - r.left - padL) / Math.max(1, w)) * (n - 1))
    setHover(Math.max(0, Math.min(n - 1, i)))
  }
  useEffect(() => { setHover(null) }, [n])
  const every = Math.max(1, Math.ceil(n / Math.max(2, Math.floor(width / 70))))
  const main = series[0]
  return (
    <div className="lc" role="img" aria-label={ariaLabel} style={{ ['--lc-l' as string]: `${padL}px` }}>
      <div className="lc-plot" ref={box} style={{ height }} onMouseMove={e => pick(e.clientX)} onMouseLeave={() => setHover(null)}
        onTouchStart={e => pick(e.touches[0].clientX)} onTouchMove={e => pick(e.touches[0].clientX)} onTouchEnd={() => setHover(null)}>
        <div className="lc-grid" aria-hidden="true">
          {ticks.map(t => <span key={t} style={{ top: y(t) }} className={t === 0 ? 'zero' : ''}><i>{(fmtAxis || fmt)(t)}</i></span>)}
        </div>
        {width > 0 && <svg width={width} height={height} className="lc-svg" aria-hidden="true">
          {refLine && <g className="lc-ref"><line x1={padL} x2={padL + w} y1={y(refLine.value)} y2={y(refLine.value)} /></g>}
          {area && main && n > 1 && <path className="lc-area" d={`${path(main.values)}L${x(n - 1)},${y(Math.max(lo, 0))}L${x(0)},${y(Math.max(lo, 0))}Z`} />}
          {series.map(s => <path key={s.key} d={path(s.values)} className={`lc-line ${s.dashed ? 'dashed' : ''} ${s.muted ? 'muted' : ''}`} />)}
          {hover !== null && <g>
            <line className="lc-cross" x1={x(hover)} x2={x(hover)} y1={padT} y2={padT + h} />
            {series.map(s => <circle key={s.key} className={`lc-dot ${s.muted ? 'muted' : ''}`} cx={x(hover)} cy={y(s.values[hover])} r={4.5} />)}
          </g>}
          {hover === null && main && n > 0 && <circle className="lc-dot" cx={x(n - 1)} cy={y(main.values[n - 1])} r={4} />}
        </svg>}
        {refLine && <span className="lc-ref-label" style={{ top: y(refLine.value) }}>{refLine.label}</span>}
        {hover !== null && <div className={`lc-tip ${x(hover) > width / 2 ? 'left' : ''}`} style={{ left: x(hover) }}>{tip(hover)}</div>}
      </div>
      <div className="lc-labels" aria-hidden="true">
        {labels.map((l, i) => <span key={i} style={{ left: x(i) }} className={(n - 1 - i) % every === 0 ? '' : 'skip'}>{l}</span>)}
      </div>
    </div>
  )
}

/** Rows of label · bar · value: what makes up a total. */
export function BarList({ rows, fmt }: { rows: { key: string; label: React.ReactNode; value: number; sub?: React.ReactNode; to?: string; icon?: React.ReactNode }[]; fmt: (v: number) => string }) {
  const max = Math.max(...rows.map(r => Math.abs(r.value)), 1)
  return (
    <div className="barlist">
      {rows.map(r => (
        <div key={r.key} className="bl-row">
          {r.icon}
          <div className="bl-main">
            <div className="bl-top"><span className="bl-label ellipsis">{r.label}</span><span className="bl-val tnum">{fmt(r.value)}</span></div>
            <div className="bl-track"><span className="bl-bar" style={{ width: `${Math.max(1.5, (Math.abs(r.value) / max) * 100)}%` }} /></div>
            {r.sub && <div className="bl-sub muted small">{r.sub}</div>}
          </div>
        </div>
      ))}
    </div>
  )
}
