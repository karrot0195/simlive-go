const copyText = (text: string) => {
  navigator.clipboard.writeText(text)
}

export { copyText }
