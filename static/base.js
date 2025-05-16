document.body.addEventListener('click', async (event) => {
  if (!event.target.classList.contains('copy-header')) return;
  
  url = ''
  url += window.location.protocol
  url += '//' + window.location.hostname
  if (window.location.port) {
    url += ':' + window.location.port
  }
  url += '/post/' + event.target.getAttribute('post')
  if (event.target.id) {
    url += '#' + event.target.id
  }
  console.log(url)
  try {
    await navigator.clipboard.writeText(url)
    
    // Do little animation
    event.target.classList.add('copied')
    setTimeout(() => {
      event.target.classList.remove('copied')
    }, 250)
  } catch (e) {
    console.log(e)
  }
})
