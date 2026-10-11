import { useSearchParams } from 'react-router'
import { Shapes } from 'lucide-react'
import { AREA_PARAM, areaFilterOptions, knownAreaId } from '../lib/areaFilter'
import { useAreas } from '../lib/useAreas'

/** Selected area from the URL (?area=), cleared when it names an area that no longer exists. */
export function useAreaFilter(): [string, (areaId: string) => void] {
  const [params, setParams] = useSearchParams()
  const { areas, loading } = useAreas()
  const areaId = knownAreaId(params.get(AREA_PARAM), areas, !loading)
  const setArea = (next: string) =>
    setParams((prev) => {
      const qs = new URLSearchParams(prev)
      if (next) qs.set(AREA_PARAM, next)
      else qs.delete(AREA_PARAM)
      return qs
    }, { replace: true })
  return [areaId, setArea]
}

/** "All areas" + one entry per functional area. */
export function AreaFilterSelect({ value, onChange }: { value: string; onChange: (areaId: string) => void }) {
  const { areas } = useAreas()
  return (
    <label className="flex min-w-0 items-center gap-2 rounded-md border border-line bg-raised px-2.5">
      <Shapes className="size-3.5 shrink-0 text-faint" />
      <select aria-label="Filter by functional area" className="w-full min-w-0 bg-transparent py-1.5 outline-none sm:w-44" value={value} onChange={(e) => onChange(e.target.value)}>
        {areaFilterOptions(areas).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
    </label>
  )
}
