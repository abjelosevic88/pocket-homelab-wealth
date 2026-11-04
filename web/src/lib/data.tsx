// The person's overview (net worth, groups, what needs a look), loaded once
// for the sidebar and pickers; any change in the app ("pwl:changed") reloads it.
import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, V1 } from './api'
import type { Asset, Overview } from './types'

interface Data { overview: Overview | null; assets: Asset[]; reload: () => void }
const Ctx = createContext<Data>({ overview: null, assets: [], reload: () => {} })
export const useData = () => useContext(Ctx)

/** Something was added or changed: lists and pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('pwl:changed'))
export function useOnChange(cb: () => void) {
  const ref = React.useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('pwl:changed', h)
    return () => window.removeEventListener('pwl:changed', h)
  }, [])
}

export function DataProvider({ children }: { children: React.ReactNode }) {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [assets, setAssets] = useState<Asset[]>([])
  const reload = useCallback(() => {
    api.get<Overview>(`${V1}/overview`).then(setOverview).catch(() => {})
    api.get<{ assets: Asset[] }>(`${V1}/assets`).then(r => setAssets(r.assets)).catch(() => {})
  }, [])
  useEffect(() => { reload() }, [reload])
  useOnChange(reload)
  return <Ctx.Provider value={{ overview, assets, reload }}>{children}</Ctx.Provider>
}
