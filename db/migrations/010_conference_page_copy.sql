ALTER TABLE conferences
  ADD COLUMN IF NOT EXISTS hero_title text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS hero_caption text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS about_title text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS about_body text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS about_body_2 text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS venue_title text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS venue_subtitle text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS venue_body text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS hotels_intro text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS local_ticket_body text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS speakers_title text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS speakers_body text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS map_embed_url text NOT NULL DEFAULT '';

UPDATE conferences
SET hero_title = $$<span class="font-bitcoin">bitcoin++</span> works in public$$,
    about_title = $$Why Open Source?$$,
    about_body = $$Bitcoin stands amongst the great decentralized protocols. Money is global, bitcoin makes it open source. This June 2026 we're building the first showcase of the power of open protocols in Nairobi, Kenya. With the help of a local team, we're putting together a celebration of working in public, shining a light on how contributions to the global protocols of Bitcoin are reshaping the developer ecosystem in Africa and beyond.$$,
    about_body_2 = $$Join us for four days of building and working in public.$$,
    venue_title = $$Open Sourcing Nairobi$$,
    venue_subtitle = $$Experience Kenya, go on Safari, submit a pull request$$,
    venue_body = $$<span class="font-bitcoin">bitcoin++</span> is excited to be bringing our very first open source edition to Nairobi, Kenya$$,
    hotels_intro = $$The conference will be held at Pride Inn Azure Towers in Nairobi. Here are some nearby hotels.$$,
    local_ticket_body = $$Are you a Kenyan citizen? This ticket price is for you! Students and developers are encouraged to use the code OSSTUDENT at checkout. Proof of citizenship may be required at check-in.$$,
    speakers_title = $$Who's Coming$$,
    speakers_body = $$<span class="font-bitcoin">bitcoin++</span>'s first ever open source edition will have an all-star lineup of devs, tinkerers, and builders from around the globe.$$,
    map_embed_url = $$https://www.google.com/maps/embed/v1/place?key=AIzaSyBs_ErgLEfaa9HBJfMzdm45JPhQgdPAMwc&q=PrideInn%20Azure%20Towers%20Nairobi&zoom=16$$
WHERE tag = 'nairobi';
