-- +goose Up
-- The operator's Terms of Service, Privacy Policy, and Imprint as Markdown,
-- edited by administrators. Every deployment is its own controller, so the
-- seeded rows are generic samples (`sample`) with bracketed placeholders for
-- operator facts; the public pages say so until an administrator saves them.
CREATE TABLE legal_documents (
    kind text PRIMARY KEY CHECK (kind IN ('terms', 'privacy', 'imprint')),
    body text NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 20000),
    sample boolean NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Bodies contain line-ending semicolons; keep goose from splitting them.
-- +goose StatementBegin
INSERT INTO legal_documents (kind, sample, body) VALUES
('terms', true, $md$
## 1. The service

This LANsurfer instance helps players at our LAN events find each other for games. It is operated by:

[Operator name]  
[Postal address]  
Email: [contact email]

The service is free. We aim to keep it available but do not guarantee availability.

## 2. Accounts

You log in with an existing account from a supported login provider. That provider's terms continue to apply to your account there. You choose a Game Nick, which is your public name on this instance.

## 3. Acceptable use

Do not:

- impersonate another person, including through your Game Nick;
- harass, threaten, or discriminate against other players;
- post hateful, illegal, or sexually explicit content, or spam;
- interfere with the service or try to access data that is not yours.

## 4. Your content

Your Game Nick, the Game Requests you create or join, and your messages in a Game Request chat are visible to other players of this instance. You are responsible for what you post.

## 5. Moderation

We may remove content or suspend accounts that break these terms.

## 6. Liability

[State your liability terms and governing law.]

## 7. Changes

We may update these terms. The date at the top shows the latest version.

## 8. Contact and reporting

For support, to report abuse or a policy violation, or for data requests, email [contact email].

To report a bug in the LANsurfer software, open an issue at <https://github.com/convention-x-treme/lansurfer/issues>.
$md$),
('privacy', true, $md$
This policy explains which personal data this LANsurfer instance processes, why, for how long, and how you can exercise your rights under the EU General Data Protection Regulation (GDPR).

## 1. Controller and contact

The controller responsible for this LANsurfer instance is:

[Operator name]  
[Postal address]  
Email: [contact email]

For any privacy question or request, write to the email address above.

## 2. What LANsurfer stores

- **Account:** an internal user ID, your Game Nick, your role (player or administrator), and when the account was created and last changed.
- **Linked login accounts:** for each account you log in with (OpenID Connect, Discord, or Twitch), the provider type, the provider's user ID, and the display name the provider reports. The display name is private and may be offered as a Game Nick suggestion; it is never shown to other players. An email address is stored only when a generic OpenID Connect provider supplies one.
- **Discord login:** LANsurfer requests only the `identify` scope, never your Discord email address, and does not keep Discord access or refresh tokens after the login completes.
- **Profile pictures:** the picture reference a login provider reports, and which one you chose as your avatar. If you choose one, other players see it next to your Game Nick. The server caches a chosen picture.
- **Sessions:** a hashed session token with its creation, last-use, and expiry times. A session expires after 7 days. Session records remain stored until you log out or your account is deleted.
- **Login flows:** short-lived login state, valid for 10 minutes and removed after it expires.
- **Your activity:** Game Requests you create, join, or leave, their chat messages and session information, the Games you follow, the Game Requests you recently viewed, and the history of changes to each Game Request.
- **Notifications:** if you enable Web Push, your notification preferences and, per browser, a device label and the push subscription (stored encrypted).
- **Server logs:** for each request, the time, your IP address, the request method and path, the response status, response size and duration, and a request ID. Error entries may add a technical error message. [State how long your server logs are kept.]

## 3. Cookies

LANsurfer sets only cookies it needs to work. It uses no analytics, advertising, or tracking cookies and no other browser storage.

- `lansurfer_session`: keeps you logged in; until the session expires (7 days) or you log out.
- `lansurfer_csrf`: protects forms against cross-site request forgery; 7 days.
- `lansurfer_auth_binding`: binds a login to the browser that started it; 30 minutes.
- `lansurfer_return`: returns you to the page you came from after login; 15 minutes.
- `lansurfer_device`: identifies this browser's Web Push subscription; 400 days.
- `lansurfer_theme`: remembers your light or dark theme choice; 1 year.

## 4. Purposes and legal bases

- Providing your account, login, notifications, and the Game Request service: Art. 6(1)(b) GDPR.
- Keeping the service secure and preventing abuse through sessions and server logs: Art. 6(1)(f) GDPR, based on our legitimate interest in a secure service.

## 5. Recipients and third parties

- **Login providers:** when you log in, you are sent to the provider you choose. That provider's own privacy policy applies to its processing.
- **Hosting:** [Describe where the server is hosted, which providers are involved, and who has access to the data.]
- **Game covers and avatars:** the server fetches game cover images from IGDB and chosen profile pictures from the login provider. Your browser never contacts these services for images.
- **Push notifications:** if you enable Web Push, notifications are delivered through your browser vendor's push service.

We do not sell your data or use it for advertising.

## 6. Retention and deletion

We keep your account data while your account exists. Unlinking a login account removes that linked account's data. You can delete your whole account at any time on your account page.

## 7. Your rights

You have the right to access (Art. 15), rectification (Art. 16), erasure (Art. 17), restriction of processing (Art. 18), data portability (Art. 20), and to object (Art. 21). To exercise them, write to the contact email above. You may also lodge a complaint with a data protection supervisory authority, for example [competent supervisory authority].

## 8. Changes

We update this policy when our data processing changes. The date at the top shows the latest version.
$md$),
('imprint', true, $md$
[Operator name]  
[Postal address]

Represented by: [name]

Email: [contact email]

[Register entry, VAT ID, or other details your jurisdiction requires.]
$md$);
-- +goose StatementEnd

-- +goose Down
DROP TABLE legal_documents;
