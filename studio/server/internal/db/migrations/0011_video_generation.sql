-- Video generation with Google's Gemini Omni Flash (the generate_video tool). Omni bills by tokens: $1.50 per 1M
-- input and $17.50 per 1M output (video) tokens, about $0.10 per second of 720p video. Text and thinking output is
-- cheaper ($9.00), so charging all output at the video rate slightly overestimates. Cached input is not offered.
-- The Video Producer gets the tool, and its goal, backstory and iteration limit change to producing the clips,
-- but only where they are still as 0007 seeded them, so your own edits are kept.

-- +goose Up
-- +goose StatementBegin
INSERT INTO model_prices (model, input_per_mtok, cached_input_per_mtok, output_per_mtok, notes) VALUES
    ('gemini-omni-1.1-flash', 1.50, 1.50, 17.50,
     'Video output billed at 5,792 tokens per second of 720p (1,931 at 360p, 8,688 at 1080p, 17,376 at 4k). https://ai.google.dev/gemini-api/docs/pricing')
ON CONFLICT (model) DO NOTHING;

UPDATE agents SET tools = array_append(tools, 'generate_video'), updated_at = now()
WHERE id = '22222222-0000-4000-8000-000000000126' AND NOT ('generate_video' = ANY (tools));

UPDATE agents SET max_iter = 20, updated_at = now()
WHERE id = '22222222-0000-4000-8000-000000000126' AND max_iter = 10;

UPDATE agents SET
    role = $s$Video Producer (Gemini Omni Flash)$s$,
    updated_at = now()
WHERE id = '22222222-0000-4000-8000-000000000126'
  AND role = $s$Video Script, Storyboard and AI Video Prompt Producer$s$;

UPDATE agents SET
    goal = $s$Produce short training, marketing and explainer videos: a timed script and storyboard, then each scene generated as a video clip with Gemini Omni Flash and saved to the workspace$s$,
    updated_at = now()
WHERE id = '22222222-0000-4000-8000-000000000126'
  AND goal = $s$Plan short training, marketing and explainer videos: script, storyboard, shot list, on-screen text, voice-over, and ready-to-paste prompts for AI video generation tools$s$;

UPDATE agents SET
    backstory = $s$A video producer who makes training and explainer videos with Google's Gemini Omni Flash. You write tight scripts timed to the second and break them into scenes of 3 to 10 seconds, each with visuals, camera direction, captions and narration. For every scene you write one detailed prompt (subject, action, setting, camera, lighting, style, and the sound or speech to hear) that repeats the descriptions of characters and style so scenes stay consistent. Then you generate each scene once with the Generate Video tool, at 720p unless asked otherwise, saving them in order as videos/scene-01.mp4, videos/scene-02.mp4 and so on, and save the script and storyboard alongside them. Every clip costs money, so you finish the script before generating anything and never regenerate a clip just to try a variation. If a clip is refused for budget or safety, you say so and keep its prompt in the storyboard.$s$,
    updated_at = now()
WHERE id = '22222222-0000-4000-8000-000000000126'
  AND backstory = $s$A video producer who makes training and explainer videos with Google's Gemini video models (Veo). You write tight scripts timed to the second and break them into scenes of at most 8 seconds, each with visuals, camera direction, captions and narration. For every scene you write one detailed generation prompt (subject, action, setting, camera, lighting, style, audio) that keeps characters, style and aspect ratio consistent from scene to scene.$s$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Puts the Video Producer back where 0007 left it (only the fields this migration still owns) and removes the Omni
-- price. If you had added a gemini-omni-1.1-flash price before this migration, it is removed too.
UPDATE agents SET tools = array_remove(tools, 'generate_video'), updated_at = now()
WHERE id = '22222222-0000-4000-8000-000000000126';

UPDATE agents SET max_iter = 10, updated_at = now()
WHERE id = '22222222-0000-4000-8000-000000000126' AND max_iter = 20;

UPDATE agents SET role = $s$Video Script, Storyboard and AI Video Prompt Producer$s$, updated_at = now()
WHERE id = '22222222-0000-4000-8000-000000000126' AND role = $s$Video Producer (Gemini Omni Flash)$s$;

UPDATE agents SET
    goal = $s$Plan short training, marketing and explainer videos: script, storyboard, shot list, on-screen text, voice-over, and ready-to-paste prompts for AI video generation tools$s$,
    updated_at = now()
WHERE id = '22222222-0000-4000-8000-000000000126'
  AND goal = $s$Produce short training, marketing and explainer videos: a timed script and storyboard, then each scene generated as a video clip with Gemini Omni Flash and saved to the workspace$s$;

UPDATE agents SET
    backstory = $s$A video producer who makes training and explainer videos with Google's Gemini video models (Veo). You write tight scripts timed to the second and break them into scenes of at most 8 seconds, each with visuals, camera direction, captions and narration. For every scene you write one detailed generation prompt (subject, action, setting, camera, lighting, style, audio) that keeps characters, style and aspect ratio consistent from scene to scene.$s$,
    updated_at = now()
WHERE id = '22222222-0000-4000-8000-000000000126'
  AND backstory LIKE $s$A video producer who makes training and explainer videos with Google's Gemini Omni Flash.%$s$;

DELETE FROM model_prices WHERE model = 'gemini-omni-1.1-flash';
-- +goose StatementEnd
