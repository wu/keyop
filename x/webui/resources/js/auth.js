// auth.js — Passkey registration and authentication for keyop

const elMessage = document.getElementById('login-message');
const elLoginBtn = document.getElementById('login-btn');
const elRegisterBtn = document.getElementById('register-btn');
const elError = document.getElementById('login-error');

// --- Base64url helpers ---

function bufToB64url(buf) {
    const bytes = new Uint8Array(buf);
    let str = '';
    for (const b of bytes) str += String.fromCharCode(b);
    return btoa(str).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function b64urlToBuf(str) {
    str = str.replace(/-/g, '+').replace(/_/g, '/');
    while (str.length % 4) str += '=';
    const bin = atob(str);
    const buf = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) buf[i] = bin.charCodeAt(i);
    return buf.buffer;
}

// Decode only the specific binary fields that the WebAuthn browser API expects
// as ArrayBuffer. All other fields (strings, objects, arrays) are left as-is.

function prepareRegistrationOptions(json) {
    const pk = json.publicKey;
    pk.challenge = b64urlToBuf(pk.challenge);
    pk.user.id = b64urlToBuf(pk.user.id);
    if (pk.excludeCredentials) {
        pk.excludeCredentials = pk.excludeCredentials.map(c => ({...c, id: b64urlToBuf(c.id)}));
    }
    return json;
}

function prepareLoginOptions(json) {
    const pk = json.publicKey;
    pk.challenge = b64urlToBuf(pk.challenge);
    if (pk.allowCredentials) {
        pk.allowCredentials = pk.allowCredentials.map(c => ({...c, id: b64urlToBuf(c.id)}));
    }
    return json;
}

function setError(msg) {
    elError.textContent = msg;
    elError.style.display = msg ? '' : 'none';
}

function setMessage(msg) {
    elMessage.textContent = msg;
}

function setLoading(btn, loading) {
    btn.disabled = loading;
    const span = btn.querySelector('span');
    let spinner = btn.querySelector('.login-spinner');
    if (loading) {
        if (!spinner) {
            spinner = document.createElement('div');
            spinner.className = 'login-spinner';
            btn.insertBefore(spinner, span);
        }
    } else {
        if (spinner) spinner.remove();
    }
}

// --- Registration ---

async function doRegister() {
    setError('');
    setLoading(elRegisterBtn, true);
    try {
        const beginResp = await fetch('/auth/register/begin', {method: 'POST'});
        if (!beginResp.ok) throw new Error(await beginResp.text());
        const options = prepareRegistrationOptions(await beginResp.json());

        const credential = await navigator.credentials.create({publicKey: options.publicKey});
        if (!credential) throw new Error('No credential returned');

        const finishResp = await fetch('/auth/register/finish', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({
                id: credential.id,
                rawId: bufToB64url(credential.rawId),
                type: credential.type,
                response: {
                    attestationObject: bufToB64url(credential.response.attestationObject),
                    clientDataJSON: bufToB64url(credential.response.clientDataJSON),
                    transports: credential.response.getTransports ? credential.response.getTransports() : [],
                },
            }),
        });
        if (!finishResp.ok) throw new Error(await finishResp.text());
        window.location.replace('/');
    } catch (err) {
        setError(err.message || 'Registration failed');
    } finally {
        setLoading(elRegisterBtn, false);
    }
}

// --- Login ---

async function doLogin() {
    setError('');
    setLoading(elLoginBtn, true);
    try {
        const beginResp = await fetch('/auth/login/begin', {method: 'POST'});
        if (!beginResp.ok) throw new Error(await beginResp.text());
        const options = prepareLoginOptions(await beginResp.json());

        const assertion = await navigator.credentials.get({publicKey: options.publicKey});
        if (!assertion) throw new Error('No assertion returned');

        const finishResp = await fetch('/auth/login/finish', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({
                id: assertion.id,
                rawId: bufToB64url(assertion.rawId),
                type: assertion.type,
                response: {
                    authenticatorData: bufToB64url(assertion.response.authenticatorData),
                    clientDataJSON: bufToB64url(assertion.response.clientDataJSON),
                    signature: bufToB64url(assertion.response.signature),
                    userHandle: assertion.response.userHandle
                        ? bufToB64url(assertion.response.userHandle)
                        : null,
                },
            }),
        });
        if (!finishResp.ok) throw new Error(await finishResp.text());
        window.location.replace('/');
    } catch (err) {
        if (err.name !== 'NotAllowedError') {
            setError(err.message || 'Authentication failed');
        }
    } finally {
        setLoading(elLoginBtn, false);
    }
}

// --- Init ---

async function init() {
    try {
        const status = await fetch('/auth/status').then(r => r.json());

        if (status.authenticated) {
            window.location.replace('/');
            return;
        }

        if (!status.registered) {
            setMessage('Register a passkey to protect your keyop instance.');
            elRegisterBtn.style.display = '';
            elRegisterBtn.addEventListener('click', doRegister);
        } else {
            setMessage('Use your passkey to sign in.');
            elLoginBtn.style.display = '';
            elLoginBtn.addEventListener('click', doLogin);
            // Auto-trigger if passkeys are supported.
            if (window.PublicKeyCredential) {
                doLogin();
            }
        }
    } catch (err) {
        setMessage('');
        setError('Failed to check auth status: ' + err.message);
    }
}

init();

