export function isElement<T extends keyof HTMLElementTagNameMap>(
  node: Node,
  tagName: T
): node is HTMLElementTagNameMap[T] {
  return (
    node.nodeType === node.ELEMENT_NODE &&
    (node as Element).tagName === tagName.toUpperCase()
  )
}
