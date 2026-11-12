/** Library metadata only. The native host embeds the same registered npm SVG
 * assets; no SVG paths, markup, file paths or React trees cross the protocol. */
export interface IIconPreviewEntry {
  readonly id: string
  readonly name: string
  readonly height: number
  readonly width: number
}
type OcticonsLibrary = Record<
  string,
  {
    readonly symbol: string
    readonly heights: Record<
      string,
      {
        readonly options: { readonly height: string; readonly width: string }
      }
    >
  }
>

export function getIconPreviewCatalog(): ReadonlyArray<IIconPreviewEntry> {
  const icons: OcticonsLibrary = require('@primer/octicons')
  return Object.keys(icons)
    .sort()
    .flatMap(name => {
      const icon = icons[name]
      return Object.entries(icon.heights).map(([height, data]) => ({
        id: `${icon.symbol}-${height}`,
        name,
        height: parseInt(data.options.height, 10),
        width: parseInt(data.options.width, 10),
      }))
    })
}
