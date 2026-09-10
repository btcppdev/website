(() => {
    const form = document.querySelector('[data-social-form]');
    if (!form) return;
    const submit = form.querySelector('[data-social-submit]');
    const editors = [];
    const carousel = form.querySelector('[data-carousel-preview]');
    const carouselSummary = form.querySelector('[data-carousel-summary]');
    function renderCarousel() {
        if (!carousel) return;
        const slides = [];
        let videoPosts = 0;
        // Editors follow the server's sponsor-level/name ordering in the page.
        editors.filter(editor => editor.sponsor && editor.selected() && editor.copy.value !== '').forEach(editor => {
            const nodes = editor.items();
            if (nodes.some(node => node.dataset.kind === 'video')) { videoPosts++; return; }
            nodes.forEach(node => {
                const source = node.querySelector('img');
                if (!source) return;
                const slide = document.createElement('li');
                const link = document.createElement('a');
                link.href = source.src;
                link.target = '_blank';
                link.rel = 'noopener';
                const image = document.createElement('img');
                image.src = source.src;
                image.alt = editor.name + ' — carousel image ' + (slides.length + 1);
                image.loading = 'lazy';
                link.append(image);
                const caption = document.createElement('p');
                caption.textContent = (slides.length + 1) + '. ' + editor.name;
                slide.append(link, caption);
                slides.push(slide);
            });
        });
        carousel.replaceChildren(...slides);
        carouselSummary.textContent = slides.length ? slides.length + ' image(s), shown in posting order. Edit media in the sponsor rows above; click an image to view it full size.' : 'Select sponsors with images above to preview the carousel.';
        if (videoPosts) carouselSummary.textContent += ' ' + videoPosts + ' video sponsor(s) will be posted separately.';
        if (slides.length && !form.querySelector('[name="text_sponsor_batch"]').value) carouselSummary.textContent += ' Add carousel post text to queue these images.';
    }
    let pending = 0;
    let submitting = false;
    const updateSubmit = () => {
        submit.disabled = submitting || pending > 0 || editors.some(editor => editor.selected() && !editor.valid());
        renderCarousel();
    };

    form.querySelectorAll('[data-social-media]').forEach(container => {
        const list = container.querySelector('[data-media-list]');
        const value = container.querySelector('[data-media-items]');
        const fileInput = container.querySelector('[data-media-file]');
        const status = container.querySelector('[data-media-status]');
        const validation = container.querySelector('[data-media-validation]');
        const selection = container.closest('[data-social-row]').querySelector('input[type="checkbox"][data-selection-group]');
        let valid = true;
        const items = () => Array.from(list.children);
        function sync() {
            const nodes = items();
            const media = nodes.map(node => ({ source: node.dataset.source, url: node.dataset.url, kind: node.dataset.kind }));
            value.value = JSON.stringify(media);
            const mixedVideo = media.some(asset => asset.kind === 'video') && media.length > 1;
            valid = !mixedVideo && media.length > 0 && media.length <= 20;
            validation.textContent = mixedVideo ? 'A video must be the only attachment. Remove the images or remove the video.' :
                media.length === 0 ? 'Add at least one attachment for Instagram.' : media.length > 20 ? 'Use at most 20 attachments.' : '';
            nodes.forEach((node, index) => {
                node.querySelector('[data-media-position]').textContent = (index + 1) + '. ';
                node.querySelector('[data-media-up]').disabled = index === 0;
                node.querySelector('[data-media-down]').disabled = index === nodes.length - 1;
            });
            updateSubmit();
        }
        function controls(node) {
            const label = node.querySelector('[data-media-label]').textContent;
            const position = document.createElement('span');
            position.dataset.mediaPosition = '';
            node.querySelector('[data-media-label]').prepend(position);
            const actions = document.createElement('div');
            actions.className = 'mt-2 flex gap-2';
            for (const [name, attr, action] of [
                ['Move left', 'mediaUp', () => { const previous = node.previousElementSibling; if (previous) list.insertBefore(node, previous); }],
                ['Move right', 'mediaDown', () => { const next = node.nextElementSibling; if (next) list.insertBefore(next, node); }],
                ['Remove', 'mediaRemove', () => node.remove()]
            ]) {
                const button = document.createElement('button');
                button.type = 'button';
                button.textContent = name;
                button.dataset[attr] = '';
                button.className = 'rounded border border-gray-300 px-2 py-1 text-xs';
                button.setAttribute('aria-label', name + ' ' + label);
                button.addEventListener('click', () => { action(); sync(); });
                actions.append(button);
            }
            node.append(actions);
        }
        items().forEach(controls);
        editors.push({ selected: () => selection.checked, valid: () => valid, items,
            sponsor: selection.dataset.selectionGroup === 'sponsor',
            name: container.dataset.sponsorName,
            copy: container.closest('[data-social-row]').querySelector('textarea') });
        selection.addEventListener('change', updateSubmit);
        sync();
        fileInput.addEventListener('change', async () => {
            const files = Array.from(fileInput.files);
            if (!files.length) return;
            pending++;
            fileInput.disabled = true;
            updateSubmit();
            let added = 0;
            const errors = [];
            // Reverse insertion keeps the selected files in their original order at the front.
            const uploaded = [];
            for (const file of files) {
                status.textContent = 'Uploading ' + file.name + '…';
                try {
                    if (!file.size || file.size > 300000000) throw new Error('File must be between 1 byte and 300 MB.');
                    const body = new FormData();
                    body.append('media', file);
                    const response = await fetch(form.dataset.mediaUploadUrl, { method: 'POST', body, credentials: 'same-origin' });
                    if (!response.ok) throw new Error(await response.text());
                    if (!(response.headers.get('content-type') || '').includes('application/json')) throw new Error('Sign in again and retry.');
                    const asset = await response.json();
                    if (!asset.url || !['image', 'video'].includes(asset.kind)) throw new Error('Invalid upload response.');
                    const node = document.createElement('li');
                    node.dataset.mediaItem = '';
                    node.dataset.source = 'upload';
                    node.dataset.url = asset.url;
                    node.dataset.kind = asset.kind;
                    node.className = 'mb-3 shrink-0 rounded border border-gray-200 p-2';
                    const label = document.createElement('strong');
                    label.dataset.mediaLabel = '';
                    label.className = 'block text-xs';
                    label.textContent = file.name;
                    const preview = document.createElement(asset.kind === 'video' ? 'video' : 'img');
                    preview.src = asset.url;
                    preview.className = 'w-32 rounded';
                    if (asset.kind === 'video') { preview.controls = true; preview.preload = 'metadata'; }
                    else preview.alt = file.name;
                    node.append(label, preview);
                    controls(node);
                    uploaded.push(node);
                    added++;
                } catch (error) { errors.push(file.name + ': ' + error.message); }
            }
            list.prepend(...uploaded);
            pending--;
            fileInput.disabled = false;
            fileInput.value = '';
            status.textContent = (added ? added + ' file(s) added. ' : '') + (errors.length ? 'Not added: ' + errors.join('; ') : '');
            sync();
        });
    });
    // Select All changes checkbox properties without emitting their change events.
    form.addEventListener('click', updateSubmit);
    form.addEventListener('input', renderCarousel);
    form.addEventListener('submit', event => {
        updateSubmit();
        if (submit.disabled || !window.confirm('Queue selected posts to Buffer?')) event.preventDefault();
        else {
            submitting = true;
            updateSubmit();
            submit.textContent = 'Queueing posts…';
        }
    });
})();
