const chooseInput = document.getElementById("post-files");
const uploadForm = document.getElementById("upload-form");
const submitPost = document.getElementById("submit-post");
const fileList = document.getElementById("file-list");

chooseInput.addEventListener("change", (event) => {
  // Add a header
  const hdr = document.createElement("h3");
  hdr.innerText = `Uploading ${chooseInput.files.length} files`;

  // Add list of files
  const list = document.createElement("ul");
  for (const file of chooseInput.files) {
    const node = document.createElement("li");
    const em = document.createElement("em");
    em.innerText = file.name;
    node.appendChild(em);
    list.appendChild(node);
  }
  fileList.replaceChildren(hdr, list);

  // Show submit button
  submitPost.style.visibility = "visible";
});

const sendData = async (url, formData) => {
  const response = await fetch(url, {
    method: "POST",
    body: formData,
  });

  return response.text();
};

const badUpload = () => {
  const hdr = document.createElement("h3");
  const em = document.createElement("em");
  em.innerText = "Bad upload!";
  hdr.appendChild(em);
  fileList.replaceChildren(hdr);
};

submitPost.style.visibility = "hidden";
uploadForm.addEventListener("submit", async (event) => {
  event.preventDefault();

  const hdr = document.createElement("h3");
  const em = document.createElement("em");
  em.innerText = "Uploading post...";
  hdr.appendChild(em);
  fileList.replaceChildren(hdr);

  try {
    const content = (await sendData("/admin/upload", new FormData(uploadForm))).split(
      "\n",
    );
    let href = `${window.location.protocol}//${window.location.hostname}`;
    if (window.location.port) {
      href += `:${window.location.port}`;
    }
    href += `/post/${content[1]}`;

    const hdr = document.createElement("h3");
    const span = document.createElement("span");
    span.innerText = "Post uploaded at: ";
    hdr.appendChild(span);
    const link = document.createElement("a");
    link.href = href;
    link.innerText = content[0];
    hdr.appendChild(link);
    fileList.replaceChildren(hdr);
  } catch {
    badUpload();
  } finally {
    submitPost.style.visibility = "hidden";
    uploadForm.reset();
    htmx.ajax("GET", "/admin/posts", "#posts-list");
  }
});

const updatePost = async (event) => {
  if (chooseInput.files.length == 0) {
    const hdr = document.createElement("h3");
    const em = document.createElement("em");
    em.innerText = "Choose files to update the post";
    hdr.appendChild(em);
    fileList.replaceChildren(hdr);
    window.scrollTo({ top: 0, behavior: "smooth" });
    return;
  }
  
  const title = event.target.getAttribute("title");
  if (!window.confirm(`Are you sure you want to update post '${title}'?`)) {
    submitPost.style.visibility = "hidden";
    uploadForm.reset();
    fileList.replaceChildren();
    return;
  }

  try {
    const post = event.target.getAttribute("post");
    const content = await sendData("/admin/update/" + post, new FormData(uploadForm));
    const entry = document.getElementById(`post-${post}`);
    entry.outerHTML = content;
  } catch {
    badUpload();
  } finally {
    submitPost.style.visibility = "hidden";
    uploadForm.reset();
    fileList.replaceChildren();
  }
};
