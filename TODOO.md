
Buatkan fitur notes seperti dibawah ini ubah ke Go. buat agar optimal, efisien, ramah ram dan cpu. berikan laporan finalnya di OUTPUT.md 

1. !get !cat #
2. !save
3. !lsnote
4. !delnote
5. !clearnote
6. !nofm -html atau -md
7. !exportnote
8. !importnote

exportnote dan importnote hapus penggunaan PASSWORD Notes.

"""Userbot notes with formatting and bot-authored inline navigation buttons."""

import asyncio
import base64
import hashlib
import hmac
import re
from collections import OrderedDict
from html import escape
from io import BytesIO

from rex.modules.note_crypto import (
    InvalidToken,
    MAX_NOTE_EXPORT_BYTES,
    NOTE_EXPORT_MAGIC,
    decrypt_notes_export,
    encrypt_notes_export,
)
from pyrogram import filters
from pyrogram.enums import ChatType, MessageEntityType, ParseMode
from pyrogram.parser import Parser
from pyrogram.types import (
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    InlineQueryResultArticle,
    InlineQueryResultCachedAnimation,
    InlineQueryResultCachedAudio,
    InlineQueryResultCachedDocument,
    InlineQueryResultCachedPhoto,
    InlineQueryResultCachedSticker,
    InlineQueryResultCachedVideo,
    InlineQueryResultCachedVoice,
    InputMediaAnimation,
    InputMediaAudio,
    InputMediaDocument,
    InputMediaPhoto,
    InputMediaVideo,
    InputTextMessageContent,
    MessageEntity,
)

from rex import bot, dp, ubot
from rex.config import BOT_TOKEN
from rex.database.personal import is_sudo_user
from rex.database.notes import (
    clear_notes,
    delete_note,
    get_note,
    list_notes,
    save_note,
)
from rex.keyboard import listen


NOTES_CACHE_CHANNEL = "AiotdCaches"
NOTES_CACHE_TOPIC_ID = 7157
NOTE_EXPORT_MAX_PASSWORD_LENGTH = 256
NOTE_EXPORT_MAX_NOTES = 500
NOTES_PAGE_SIZE = 20
NOTES_LIST_MESSAGE_LIMIT = 32
_NOTES_LIST_MESSAGES = OrderedDict()
NOTE_PASSWORD_DELETE_FAILURE_MESSAGE = (
    "Saya tidak dapat memastikan pesan password sudah dihapus. "
    "Hapus balasan password itu secara manual sebelum mencoba lagi."
)


class NotePasswordDeleteError(Exception):
    """Raised when Telegram does not confirm deletion of a password reply."""


__MODULE__ = "Notes"


async def _request_note_password(client, message, action):
    if getattr(getattr(message, "chat", None), "type", None) != ChatType.PRIVATE:
        return None

    actor_id = (
        getattr(getattr(client, "me", None), "id", None)
        if getattr(message, "outgoing", False)
        else getattr(getattr(message, "from_user", None), "id", None)
    )
    if actor_id is None:
        return None

    prompt_message_id = None
    reply_to_prompt = filters.create(
        lambda _filter, _client, response: (
            prompt_message_id is not None
            and getattr(
                getattr(response, "reply_to_message", None), "id", None
            ) == prompt_message_id
        ),
        name="aiotd_note_password_reply",
    )
    listener_task = asyncio.create_task(
        listen(
            client,
            message.chat.id,
            filters=filters.text & reply_to_prompt,
            timeout=120,
            user_id=int(actor_id),
        )
    )
    prompt = None
    try:
        # Let Pyrofork register the listener before the prompt can be answered.
        await asyncio.sleep(0)
        prompt = await message.reply(
            f"Balas pesan ini dengan password untuk {action} catatan (minimal 8 karakter). "
            "Pesan password akan dihapus jika memungkinkan."
        )
        prompt_message_id = getattr(prompt, "id", None)
        if prompt_message_id is None:
            raise RuntimeError("password prompt did not return a message ID")
        response = await listener_task
    except asyncio.CancelledError:
        listener_task.cancel()
        await asyncio.gather(listener_task, return_exceptions=True)
        await _delete_note_password_prompt(prompt)
        raise
    except Exception:
        listener_task.cancel()
        await asyncio.gather(listener_task, return_exceptions=True)
        await _delete_note_password_prompt(prompt)
        return None

    if response is None:
        await _delete_note_password_prompt(prompt)
        return None

    if getattr(
        getattr(response, "reply_to_message", None), "id", None
    ) != prompt_message_id:
        await _delete_note_password_prompt(prompt)
        return None

    password = (getattr(response, "text", "") or "").strip()
    try:
        await response.delete()
    except asyncio.CancelledError:
        raise
    except Exception:
        raise NotePasswordDeleteError from None
    finally:
        await _delete_note_password_prompt(prompt)
    if not 8 <= len(password) <= NOTE_EXPORT_MAX_PASSWORD_LENGTH:
        return None
    return password


async def _delete_note_password_prompt(prompt):
    if prompt is None:
        return
    try:
        await prompt.delete()
    except Exception:
        pass
__HELP__ = (
    "Balas pesan dengan .save <nama> [-cb|-pop], lalu gunakan .get <nama>, #<nama>, .cat, "
    ".lsnote, .exportnote, .importnote, .delnote, atau .clearnote."
)

_NAME_RE = re.compile(r"^[\w-]{1,32}$", re.UNICODE)
_URL_BUTTON_RE = re.compile(
    r"\[([^\[\]|]+)\|\[(?:https?://[^\]]+)\]\((https?://[^)]+)\)\]",
    re.IGNORECASE,
)
_URL_BUTTON_SIMPLE_RE = re.compile(r"\[([^\[\]|]+)\|(https?://[^\]\s]+)\]", re.IGNORECASE)
_CALLBACK_BUTTON_RE = re.compile(r"\[(Callback|Back)#([\w-]{1,32})\]", re.IGNORECASE)
_POP_BUTTON_RE = re.compile(r"\[([^\[\]$]{1,40})\$([\w-]{1,32})\]")
_COMMAND_RE = re.compile(r"^\S+\s*")
_HTML_RE = re.compile(r"</?(?:b|strong|i|em|u|s|strike|code|pre|blockquote|a|tg-spoiler)(?:\s|>)", re.IGNORECASE)


def _args(message) -> str:
    return _COMMAND_RE.sub("", getattr(message, "text", None) or "", count=1).strip()


def _valid_name(name: str) -> bool:
    return bool(_NAME_RE.fullmatch(name)) and "." not in name and not name.startswith("$")


def _utf16_len(value: str) -> int:
    return len(value.encode("utf-16-le")) // 2


def _entity_to_dict(entity):
    data = {
        "type": getattr(entity.type, "name", str(entity.type)),
        "offset": entity.offset,
        "length": entity.length,
    }
    for field in ("url", "language", "custom_emoji_id", "collapsed"):
        value = getattr(entity, field, None)
        if value is not None:
            data[field] = value
    return data


def _dict_to_entity(data):
    try:
        entity_type = MessageEntityType[data["type"]]
        return MessageEntity(
            type=entity_type,
            offset=int(data["offset"]),
            length=int(data["length"]),
            url=data.get("url"),
            language=data.get("language"),
            custom_emoji_id=data.get("custom_emoji_id"),
            collapsed=data.get("collapsed"),
        )
    except (KeyError, TypeError, ValueError):
        return None


def _as_html(text: str, entity_data):
    """Convert Telegram entities to HTML so edit calls follow the .help path."""
    if _HTML_RE.search(text or ""):
        return text
    entities = [
        entity for item in entity_data or []
        if (entity := _dict_to_entity(item)) is not None
    ]
    return Parser.unparse(text or "", entities, is_html=True)


def _html_source(text: str, entities):
    """Render Telegram formatting entities as visible HTML source text."""
    if _HTML_RE.search(text or ""):
        return text
    return Parser.unparse(text or "", entities or [], is_html=True)


def _extract_buttons(text: str):
    """Remove note button directives and return their spans and button metadata."""
    found = []
    for match in _URL_BUTTON_RE.finditer(text):
        found.append((match.start(), match.end(), {"kind": "url", "label": match.group(1).strip(), "url": match.group(2)}))
    for match in _URL_BUTTON_SIMPLE_RE.finditer(text):
        found.append((match.start(), match.end(), {"kind": "url", "label": match.group(1).strip(), "url": match.group(2)}))
    for match in _CALLBACK_BUTTON_RE.finditer(text):
        found.append((match.start(), match.end(), {"kind": "callback", "label": match.group(1), "target": match.group(2).lower()}))
    for match in _POP_BUTTON_RE.finditer(text):
        found.append((match.start(), match.end(), {"kind": "popup", "label": match.group(1).strip(), "target": match.group(2).lower()}))

    found.sort(key=lambda item: item[0])
    non_overlapping = []
    previous_end = -1
    for item in found:
        if item[0] >= previous_end:
            non_overlapping.append(item)
            previous_end = item[1]
    return non_overlapping


def _clean_text_entities(text: str, entities):
    directives = _extract_buttons(text)
    if not directives:
        return text, [_entity_to_dict(entity) for entity in (entities or [])], []

    output_parts = []
    cursor = 0
    removed = []
    buttons = []
    for start, end, button in directives:
        output_parts.append(text[cursor:start])
        removed.append((_utf16_len(text[:start]), _utf16_len(text[start:end])))
        buttons.append(button)
        cursor = end
    output_parts.append(text[cursor:])
    cleaned = "".join(output_parts)

    saved_entities = []
    for entity in entities or []:
        start, end = entity.offset, entity.offset + entity.length
        removed_before = 0
        removed_inside = 0
        for span_start, span_length in removed:
            span_end = span_start + span_length
            if span_end <= start:
                removed_before += span_length
            elif span_start < end and span_end > start:
                removed_inside += max(0, min(end, span_end) - max(start, span_start))
        new_length = entity.length - removed_inside
        if new_length > 0:
            data = _entity_to_dict(entity)
            data["offset"] = max(0, start - removed_before)
            data["length"] = new_length
            saved_entities.append(data)
    return cleaned, saved_entities, buttons


def _base36(value):
    value = int(value)
    alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
    if value == 0:
        return "0"
    result = ""
    while value:
        value, remainder = divmod(value, 36)
        result = alphabet[remainder] + result
    return result


def _note_share_signature(action, owner_id, name, audience_id):
    payload = f"{action}|{int(owner_id)}|{name.lower()}|{int(audience_id)}".encode()
    digest = hmac.new(BOT_TOKEN.encode(), payload, hashlib.sha256).digest()[:6]
    return base64.urlsafe_b64encode(digest).decode().rstrip("=")


def _note_callback_data(action, owner_id, name, audience_id):
    action_code = {"show": "s", "pop": "p", "copy": "c"}[action]
    if int(audience_id) == int(owner_id):
        return f"note:{action}:{int(owner_id)}:{name.lower()}"
    signature = _note_share_signature(action, owner_id, name, audience_id)
    return (
        f"ns:{action_code}:{_base36(owner_id)}:{name.lower()}:"
        f"{_base36(audience_id)}:{signature}"
    )


def _parse_shared_note_callback(data):
    try:
        _, action_code, owner_code, name, audience_code, signature = data.split(":", 5)
        alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
        owner_id = int(owner_code, 36)
        audience_id = int(audience_code, 36)
        action = {"s": "show", "p": "pop", "c": "copy"}[action_code]
    except (AttributeError, ValueError, KeyError):
        return None
    if not _valid_name(name):
        return None
    expected = _note_share_signature(action, owner_id, name, audience_id)
    if not hmac.compare_digest(expected, signature):
        return None
    return action, owner_id, name.lower(), audience_id


def _inline_keyboard(owner_id: int, note, audience_id=None):
    audience_id = int(owner_id if audience_id is None else audience_id)
    rows = []
    row = []
    for button in note.get("buttons", [])[:12]:
        kind = button.get("kind")
        label = button.get("label", "Buka")[:40]
        if kind == "url" and button.get("url", "").startswith(("http://", "https://")):
            row.append(InlineKeyboardButton(label, url=button["url"]))
        elif kind in {"callback", "popup"} and _valid_name(button.get("target", "")):
            action = "pop" if kind == "popup" else "show"
            row.append(
                InlineKeyboardButton(
                    label,
                    callback_data=_note_callback_data(
                        action, owner_id, button["target"].lower(), audience_id
                    ),
                )
            )
        if len(row) == 3:
            rows.append(row)
            row = []
    if row:
        rows.append(row)
    return InlineKeyboardMarkup(rows) if rows else None


def _render_options(note, no_format=False):
    text = note.get("text", "")
    if no_format:
        return ParseMode.DISABLED, []
    if _HTML_RE.search(text):
        return ParseMode.HTML, None
    entities = [] if no_format else [
        entity for item in note.get("entities", [])
        if (entity := _dict_to_entity(item)) is not None
    ]
    if entities:
        return ParseMode.DISABLED, entities
    return ParseMode.DISABLED, []


def _has_media_payload(message):
    return any(
        getattr(message, field, None)
        for field in (
            "photo", "video", "video_note", "animation", "audio", "document",
            "voice", "sticker", "contact", "location", "venue", "poll", "dice", "game",
        )
    )


def _message_content(note, no_format=False):
    parse_mode, entities = _render_options(note, no_format=no_format)
    return InputTextMessageContent(
        note.get("text", ""),
        parse_mode=parse_mode,
        entities=entities,
        disable_web_page_preview=False,
    )


async def _send_via_bot(
    client, message, note_name: str, no_format=False, audience_id=None
):
    try:
        inline = await client.get_inline_bot_results(
            bot.me.username,
            f"note_show {client.me.id} {note_name} "
            f"{'nofm' if no_format else 'fm'}"
            + (f" to={int(audience_id)}" if audience_id else ""),
        )
        if not inline.results:
            return await message.reply("Catatan tidak dapat ditampilkan lewat bot. Pastikan inline mode aktif.")
        return await message.reply_inline_bot_result(inline.query_id, inline.results[0].id)
    except Exception as exc:
        print(f"[NOTES INLINE] gagal menampilkan catatan: {type(exc).__name__}")
        return await message.reply("Gagal menampilkan catatan. Pastikan inline mode bot aktif.")


async def _get_cached_note(owner_id: int, name: str):
    note = await get_note(owner_id, name)
    if not note:
        return None, None
    if isinstance(note, str) or "cache_message_id" not in note:
        return note, None
    try:
        cached_message = await bot.get_messages(
            NOTES_CACHE_CHANNEL,
            int(note["cache_message_id"]),
        )
    except Exception as exc:
        print(f"[NOTES CACHE] gagal membaca pesan: {type(exc).__name__}")
        return note, None
    if not cached_message or getattr(cached_message, "empty", False):
        return note, None
    return note, cached_message


async def _send_direct_cached_note(client, command_message, note, no_format=False):
    """Send button-free notes as the userbot, keeping cached Telegram media and entities."""
    try:
        source = await client.get_messages(
            note.get("cache_channel", NOTES_CACHE_CHANNEL),
            int(note["cache_message_id"]),
        )
        if not source or getattr(source, "empty", False):
            return await command_message.reply("Pesan cache catatan tidak ditemukan.")

        text = source.text or source.caption or ""
        if no_format or _HTML_RE.search(text):
            if no_format:
                text = _html_source(
                    text,
                    source.entities or source.caption_entities or [],
                )
            mode = ParseMode.DISABLED if no_format else ParseMode.HTML
            entities = None
            kwargs = {
                "parse_mode": mode,
                "entities": entities,
                "reply_to_message_id": command_message.id,
                "disable_web_page_preview": False,
            }
            if source.text is not None:
                return await client.send_message(command_message.chat.id, text, **kwargs)

            caption_kwargs = {
                "caption": text,
                "parse_mode": mode,
                "caption_entities": None,
                "reply_to_message_id": command_message.id,
            }
            if source.photo:
                return await client.send_photo(command_message.chat.id, source.photo.file_id, **caption_kwargs)
            if source.video:
                return await client.send_video(command_message.chat.id, source.video.file_id, **caption_kwargs)
            if source.animation:
                return await client.send_animation(command_message.chat.id, source.animation.file_id, **caption_kwargs)
            if source.audio:
                return await client.send_audio(command_message.chat.id, source.audio.file_id, **caption_kwargs)
            if source.document:
                return await client.send_document(command_message.chat.id, source.document.file_id, **caption_kwargs)
            if source.voice:
                return await client.send_voice(command_message.chat.id, source.voice.file_id, **caption_kwargs)
            if source.sticker:
                return await client.send_sticker(
                    command_message.chat.id,
                    source.sticker.file_id,
                    reply_to_message_id=command_message.id,
                )
            if source.video_note:
                return await client.send_video_note(
                    command_message.chat.id,
                    source.video_note.file_id,
                    reply_to_message_id=command_message.id,
                )

        return await client.copy_message(
            chat_id=command_message.chat.id,
            from_chat_id=note.get("cache_channel", NOTES_CACHE_CHANNEL),
            message_id=int(note["cache_message_id"]),
            reply_to_message_id=command_message.id,
        )
    except Exception as exc:
        print(f"[NOTES COPY] gagal menyalin catatan: {type(exc).__name__}: {str(exc)[:120]}")
        return await command_message.reply("Catatan gagal disalin dari cache.")


def _cache_media_result(note_name, cached_message, owner_id, note, markup, text, entities, no_format=False):
    result_id = f"note_{owner_id}_{note_name}"[:64]
    caption_args = {
        "caption": text,
        "parse_mode": (
            ParseMode.HTML
            if not no_format and _HTML_RE.search(text or "")
            else ParseMode.DISABLED
        ),
        "caption_entities": None if _HTML_RE.search(text or "") and not no_format else entities or None,
        "reply_markup": markup,
        "id": result_id,
    }
    if cached_message.photo:
        return InlineQueryResultCachedPhoto(
            photo_file_id=cached_message.photo.file_id,
            **caption_args,
        )
    if cached_message.video or cached_message.video_note:
        video = cached_message.video or cached_message.video_note
        return InlineQueryResultCachedVideo(
            video_file_id=video.file_id,
            title=note_name,
            **caption_args,
        )
    if cached_message.animation:
        return InlineQueryResultCachedAnimation(
            animation_file_id=cached_message.animation.file_id,
            title=note_name,
            **caption_args,
        )
    if cached_message.audio:
        return InlineQueryResultCachedAudio(
            audio_file_id=cached_message.audio.file_id,
            **caption_args,
        )
    if cached_message.document:
        return InlineQueryResultCachedDocument(
            document_file_id=cached_message.document.file_id,
            title=note_name,
            **caption_args,
        )
    if cached_message.voice:
        return InlineQueryResultCachedVoice(
            voice_file_id=cached_message.voice.file_id,
            title=note_name,
            **caption_args,
        )
    if cached_message.sticker:
        return InlineQueryResultCachedSticker(
            sticker_file_id=cached_message.sticker.file_id,
            id=result_id,
            reply_markup=markup,
        )
    return None


def _input_media(cached_message, caption, entities):
    parse_mode = ParseMode.DISABLED
    if _HTML_RE.search(caption or ""):
        parse_mode = ParseMode.HTML
    common = {
        "caption": caption,
        "parse_mode": parse_mode,
        "caption_entities": None if parse_mode == ParseMode.HTML else entities or None,
    }
    if cached_message.photo:
        return InputMediaPhoto(cached_message.photo.file_id, **common)
    if cached_message.video or cached_message.video_note:
        video = cached_message.video or cached_message.video_note
        return InputMediaVideo(video.file_id, **common)
    if cached_message.animation:
        return InputMediaAnimation(cached_message.animation.file_id, **common)
    if cached_message.audio:
        return InputMediaAudio(cached_message.audio.file_id, **common)
    if cached_message.document:
        return InputMediaDocument(cached_message.document.file_id, **common)
    if cached_message.voice:
        return InputMediaDocument(cached_message.voice.file_id, **common)
    if cached_message.sticker:
        return InputMediaDocument(cached_message.sticker.file_id)
    return None


@dp.UBOT("save", sudo=True)
async def save_note_command(client, message):
    args = _args(message).split()
    if not args:
        return await message.reply("Balas pesan yang ingin disimpan dengan <code>.save nama [-cb|-pop]</code>.")

    name = args[0].lower()
    options = {value.lower() for value in args[1:] if value.startswith("-")}
    if not _valid_name(name):
        return await message.reply("Nama catatan gunakan 1–32 huruf, angka, _ atau -.")

    source = getattr(message, "reply_to_message", None)
    if not source:
        return await message.reply(
            "Balas pesan yang ingin disimpan: teks, foto, video, GIF, sticker, "
            "voice, audio, dokumen, atau pesan lain."
        )
    if not (source.text or source.caption or source.media):
        return await message.reply("Pesan tersebut tidak berisi teks atau media yang dapat disimpan.")

    mode = "popup" if "-pop" in options else "callback" if "-cb" in options else "text"
    try:
        cached_message = await client.copy_message(
            chat_id=NOTES_CACHE_CHANNEL,
            from_chat_id=source.chat.id,
            message_id=source.id,
            message_thread_id=NOTES_CACHE_TOPIC_ID,
        )
    except Exception as exc:
        error_text = str(exc).upper()
        if "THREAD" in error_text or "TOPIC" in error_text:
            try:
                cached_message = await client.copy_message(
                    chat_id=NOTES_CACHE_CHANNEL,
                    from_chat_id=source.chat.id,
                    message_id=source.id,
                    reply_to_message_id=NOTES_CACHE_TOPIC_ID,
                )
            except Exception as fallback_exc:
                print(f"[NOTES CACHE] gagal menyimpan pesan: {type(fallback_exc).__name__}")
                return await message.reply(
                    "Gagal menyimpan ke @AiotdCaches/7157. Pastikan sesi userbot dapat "
                    "mengakses channel cache dan nomor 7157 adalah topic atau pesan yang valid."
                )
        else:
            print(f"[NOTES CACHE] gagal menyimpan pesan: {type(exc).__name__}")
            return await message.reply(
                "Gagal menyimpan ke @AiotdCaches/7157. Pastikan sesi userbot "
                "dapat mengakses channel dan topic cache."
            )
    await save_note(client.me.id, name, {
        "cache_message_id": cached_message.id,
        "cache_channel": NOTES_CACHE_CHANNEL,
        "mode": mode,
    })
    return await message.reply(f"Catatan <code>{escape(name)}</code> berhasil disimpan.")


async def _show_note(client, message, name):
    name = name.lower()
    if not _valid_name(name):
        return await message.reply("Gunakan <code>.get nama</code> atau <code>#nama</code>.")
    note, cached_message = await _get_cached_note(client.me.id, name)
    if not note:
        return await message.reply(f"Catatan <code>{escape(name)}</code> tidak ditemukan.")
    if cached_message:
        cached_text = cached_message.text or cached_message.caption or ""
        _, _, buttons = _clean_text_entities(
            cached_text,
            cached_message.entities or cached_message.caption_entities or [],
        )
        if not buttons:
            return await _send_direct_cached_note(client, message, note)
    return await _send_via_bot(client, message, name)
    if isinstance(note, str):
        return await message.reply(note, parse_mode=ParseMode.DISABLED)
    if not isinstance(note, dict) or note.get("text") is None:
        return await message.reply("Pesan cache catatan tidak ditemukan di @AiotdCaches.")
    parse_mode, entities = _render_options(note)
    return await message.reply(
        note.get("text", ""),
        entities=entities or None,
        parse_mode=parse_mode,
        disable_web_page_preview=False,
    )


@dp.UBOT("get", sudo=True)
async def get_note_command(client, message):
    name = _args(message).split(maxsplit=1)[0] if _args(message) else ""
    return await _show_note(client, message, name)


async def _note_hash_allowed(_, client, message):
    if getattr(message, "outgoing", False):
        return True
    actor = getattr(message, "from_user", None)
    return bool(actor and await is_sudo_user(client.me.id, actor.id))


@ubot.on_message(
    filters.text
    & filters.regex(r"^#[\w-]{1,32}$")
    & filters.create(_note_hash_allowed),
    group=-1,
)
async def get_note_by_hash(client, message):
    text = (message.text or "").strip()
    return await _show_note(client, message, text[1:])


@dp.UBOT("cat", sudo=True)
async def cat_note_command(client, message):
    args = _args(message).split()
    name = args[0].lower() if args else ""
    if not _valid_name(name):
        return await message.reply("Format: <code>.cat nama [-nofm]</code>")
    note, cached_message = await _get_cached_note(client.me.id, name)
    if not note:
        return await message.reply(f"Catatan <code>{escape(name)}</code> tidak ditemukan.")
    if not cached_message and not (isinstance(note, dict) and note.get("text") is not None):
        return await message.reply("Pesan cache catatan tidak ditemukan di @AiotdCaches.")
    no_format = "-nofm" in args[1:]
    if cached_message:
        cached_text = cached_message.text or cached_message.caption or ""
        _, _, buttons = _clean_text_entities(
            cached_text,
            cached_message.entities or cached_message.caption_entities or [],
        )
        if not buttons:
            return await _send_direct_cached_note(client, message, note, no_format=no_format)
    return await _send_via_bot(client, message, name, no_format=no_format)


@dp.UBOT("notes|lsnote", sudo=True)
async def list_notes_command(client, message):
    user_id = int(client.me.id)
    names = await list_notes(user_id)
    if not names:
        return await message.reply("Belum ada catatan. Balas pesan dengan <code>.save nama</code>.")
    page = 0
    text, keyboard = _notes_list_page(names, user_id, page)
    chat_id = int(message.chat.id)
    key = (user_id, chat_id)
    previous_id = _NOTES_LIST_MESSAGES.get(key)
    if previous_id is not None:
        try:
            await client.edit_message_text(
                chat_id,
                previous_id,
                text,
                reply_markup=keyboard,
            )
            _NOTES_LIST_MESSAGES.move_to_end(key)
            return
        except Exception:
            _NOTES_LIST_MESSAGES.pop(key, None)

    response = await message.reply(text, reply_markup=keyboard)
    _NOTES_LIST_MESSAGES[key] = response.id
    while len(_NOTES_LIST_MESSAGES) > NOTES_LIST_MESSAGE_LIMIT:
        _NOTES_LIST_MESSAGES.popitem(last=False)


def _notes_list_page(names, user_id, page):
    total_pages = max(1, (len(names) + NOTES_PAGE_SIZE - 1) // NOTES_PAGE_SIZE)
    page = max(0, min(int(page), total_pages - 1))
    first_index = page * NOTES_PAGE_SIZE
    current_names = names[first_index:first_index + NOTES_PAGE_SIZE]
    rows = "\n".join(
        f"{first_index + index}. <code>{escape(name)}</code>"
        for index, name in enumerate(current_names, 1)
    )
    text = (
        f"<b>Daftar Catatan</b> · {first_index + 1}–{first_index + len(current_names)} "
        f"dari {len(names)}\n{rows}"
    )
    if total_pages == 1:
        return text, None

    controls = []
    if page > 0:
        controls.append(
            InlineKeyboardButton("‹ Sebelumnya", callback_data=f"notes:list:{user_id}:{page - 1}")
        )
    controls.append(InlineKeyboardButton(f"{page + 1}/{total_pages}", callback_data="notes:list:page"))
    if page + 1 < total_pages:
        controls.append(
            InlineKeyboardButton("Berikutnya ›", callback_data=f"notes:list:{user_id}:{page + 1}")
        )
    return text, InlineKeyboardMarkup([controls])


@ubot.on_callback_query(filters.regex(r"^notes:list:(?:\d+:\d+|page)$"), group=-2)
async def notes_list_page_callback(client, callback_query):
    data = callback_query.data.rsplit(":", 1)
    if data[-1] == "page":
        return await callback_query.answer("Halaman daftar catatan")

    _, _, owner_text, page_text = callback_query.data.split(":")
    owner_id = int(owner_text)
    if (
        not callback_query.from_user
        or not await is_sudo_user(owner_id, callback_query.from_user.id)
        or not getattr(client, "me", None)
        or int(client.me.id) != owner_id
    ):
        return await callback_query.answer("Daftar ini hanya dapat dibuka pemilik atau Sudo akun.", show_alert=True)

    names = await list_notes(owner_id)
    if not names:
        return await callback_query.answer("Daftar catatan sudah kosong.", show_alert=True)
    text, keyboard = _notes_list_page(names, owner_id, int(page_text))
    await callback_query.edit_message_text(text, reply_markup=keyboard)
    await callback_query.answer()


@dp.UBOT("delnote", sudo=True)
async def delete_note_command(client, message):
    value = _args(message).split(maxsplit=1)[0] if _args(message) else ""
    if not value:
        return await message.reply("Format: <code>.delnote nomor</code> atau <code>.delnote nama</code>.")

    if value.isdigit():
        names = await list_notes(client.me.id)
        if not names:
            return await message.reply("Belum ada catatan untuk dihapus.")
        index = int(value) - 1
        if index < 0 or index >= len(names):
            return await message.reply(f"Nomor catatan harus antara 1 dan {len(names)}.")
        name = names[index]
    else:
        name = value.lower()
        if not _valid_name(name):
            return await message.reply("Format: <code>.delnote nomor</code> atau <code>.delnote nama</code>.")

    if not await delete_note(client.me.id, name):
        return await message.reply(f"Catatan <code>{escape(name)}</code> tidak ditemukan.")
    return await message.reply(f"Catatan <code>{escape(name)}</code> dihapus.")


@dp.UBOT("clearnote", sudo=True)
async def clear_notes_command(client, message):
    count = await clear_notes(client.me.id)
    return await message.reply(f"{count} catatan berhasil dihapus.")


@dp.UBOT("exportnote", sudo=True)
async def export_notes_command(client, message):
    if getattr(getattr(message, "chat", None), "type", None) != ChatType.PRIVATE:
        return await message.reply("Export catatan hanya tersedia di chat privat.")

    notes = await list_notes(client.me.id, include_data=True)
    if not notes:
        return await message.reply("Tidak ada catatan untuk diexport.")
    if len(notes) > NOTE_EXPORT_MAX_NOTES:
        return await message.reply(
            f"Export maksimal {NOTE_EXPORT_MAX_NOTES} catatan per file."
        )
    try:
        password = await _request_note_password(client, message, "mengenkripsi")
    except NotePasswordDeleteError:
        return await message.reply(NOTE_PASSWORD_DELETE_FAILURE_MESSAGE)
    if not password:
        return await message.reply("Password tidak valid atau waktu input habis.")

    try:
        encrypted_payload = await encrypt_notes_export(
            notes,
            password,
            max_bytes=MAX_NOTE_EXPORT_BYTES,
        )
    except (TypeError, ValueError):
        return await message.reply(
            "Data export melebihi batas ukuran atau tidak dapat diserialisasi."
        )
    document = BytesIO(encrypted_payload)
    document.name = "aiotd-notes.json.enc"
    return await message.reply_document(
        document,
        caption=f"{len(notes)} catatan berhasil diexport dan dienkripsi.",
    )


@dp.UBOT("importnote", sudo=True)
async def import_notes_command(client, message):
    if getattr(getattr(message, "chat", None), "type", None) != ChatType.PRIVATE:
        return await message.reply("Import catatan hanya tersedia di chat privat.")

    source = getattr(message, "reply_to_message", None)
    if not source or not getattr(source, "document", None):
        return await message.reply("Balas file <code>aiotd-notes.json</code> dengan <code>.importnote</code>.")
    if source.document.file_size and source.document.file_size > MAX_NOTE_EXPORT_BYTES:
        return await message.reply("File import maksimal 2 MB.")
    try:
        stream = await client.download_media(source, in_memory=True)
        raw_payload = stream.getvalue()
        if len(raw_payload) > MAX_NOTE_EXPORT_BYTES:
            return await message.reply("File import maksimal 2 MB.")
        password = None
        if raw_payload.startswith(NOTE_EXPORT_MAGIC):
            password = await _request_note_password(client, message, "membuka")
            if not password:
                return await message.reply("Password tidak valid atau waktu input habis.")
        payload = await decrypt_notes_export(
            raw_payload,
            password or "",
            max_bytes=MAX_NOTE_EXPORT_BYTES,
        )
        records = payload.get("notes", {})
        if not isinstance(records, dict) or len(records) > NOTE_EXPORT_MAX_NOTES:
            raise ValueError("format catatan tidak valid")
        valid_records = {
            name.lower(): note for name, note in records.items()
            if _valid_name(name) and isinstance(note, (dict, str))
        }
        if len(valid_records) != len(records):
            raise ValueError("ada nama atau data catatan tidak valid")
        for name, note in valid_records.items():
            await save_note(client.me.id, name, note)
    except NotePasswordDeleteError:
        return await message.reply(NOTE_PASSWORD_DELETE_FAILURE_MESSAGE)
    except (InvalidToken, UnicodeDecodeError, ValueError, TypeError) as exc:
        print(f"[NOTES IMPORT] file tidak valid: {type(exc).__name__}")
        return await message.reply("File catatan tidak valid atau password salah.")
    except Exception as exc:
        print(f"[NOTES IMPORT] gagal membaca file: {type(exc).__name__}")
        return await message.reply("File catatan tidak valid atau gagal dibaca.")
    return await message.reply(f"{len(valid_records)} catatan berhasil diimport.")


@dp.UBOT("nofm", sudo=True)
async def no_format_command(client, message):
    source = getattr(message, "reply_to_message", None)
    if not source:
        return await message.reply("Balas pesan berformat dengan <code>.nofm</code> untuk menampilkan sumber HTML-nya.")

    text = source.text or source.caption or ""
    entities = source.entities or source.caption_entities or []
    if not text or not entities:
        return await message.reply("Pesan yang dibalas tidak memiliki teks/caption dengan entities.")

    try:
        html_text = Parser.unparse(text, entities, is_html=True)
        reply_to_message_id = message.id
        if source.photo:
            return await client.send_photo(
                message.chat.id,
                source.photo.file_id,
                caption=html_text,
                parse_mode=ParseMode.DISABLED,
                reply_to_message_id=reply_to_message_id,
            )
        if source.video:
            return await client.send_video(
                message.chat.id,
                source.video.file_id,
                caption=html_text,
                parse_mode=ParseMode.DISABLED,
                reply_to_message_id=reply_to_message_id,
            )
        if source.animation:
            return await client.send_animation(
                message.chat.id,
                source.animation.file_id,
                caption=html_text,
                parse_mode=ParseMode.DISABLED,
                reply_to_message_id=reply_to_message_id,
            )
        if source.audio:
            return await client.send_audio(
                message.chat.id,
                source.audio.file_id,
                caption=html_text,
                parse_mode=ParseMode.DISABLED,
                reply_to_message_id=reply_to_message_id,
            )
        if source.document:
            return await client.send_document(
                message.chat.id,
                source.document.file_id,
                caption=html_text,
                parse_mode=ParseMode.DISABLED,
                reply_to_message_id=reply_to_message_id,
            )
        if source.voice:
            return await client.send_voice(
                message.chat.id,
                source.voice.file_id,
                caption=html_text,
                parse_mode=ParseMode.DISABLED,
                reply_to_message_id=reply_to_message_id,
            )
        if source.text is not None:
            return await client.send_message(
                message.chat.id,
                html_text,
                parse_mode=ParseMode.DISABLED,
                reply_to_message_id=reply_to_message_id,
                disable_web_page_preview=False,
            )
        return await message.reply("Tipe media ini tidak mendukung pengiriman ulang caption HTML.")
    except Exception as exc:
        print(f"[NOFM] gagal mengonversi pesan: {type(exc).__name__}: {str(exc)[:120]}")
        return await message.reply("Pesan gagal dikonversi ke HTML.")


@dp.INLINE(r"^note_show\s+\d+\s+[\w-]{1,32}(?:\s+(?:fm|nofm))?(?:\s+to=\d+)?$")
async def answer_note_inline(client, inline_query):
    parts = inline_query.query.split()
    try:
        owner_id, name = int(parts[1]), parts[2].lower()
    except (IndexError, ValueError):
        return await inline_query.answer([], cache_time=0, is_personal=True)
    if not inline_query.from_user or not await is_sudo_user(owner_id, inline_query.from_user.id):
        return await inline_query.answer([], cache_time=0, is_personal=True)
    # Notes sent into a chat are public: any reader can use their buttons.
    # PMpermit passes an explicit recipient and remains recipient-scoped.
    audience_id = 0
    for part in parts[3:]:
        if part.startswith("to="):
            try:
                audience_id = int(part[3:])
            except ValueError:
                return await inline_query.answer([], cache_time=0, is_personal=True)
            if audience_id <= 0:
                return await inline_query.answer([], cache_time=0, is_personal=True)
    note, cached_message = await _get_cached_note(owner_id, name)
    if not note:
        return await inline_query.answer([], cache_time=0, is_personal=True)
    if isinstance(note, dict) and note.get("cache_message_id") and not cached_message:
        return await inline_query.answer([], cache_time=0, is_personal=True)
    no_format = len(parts) > 3 and parts[3] == "nofm"
    if cached_message:
        source_text = cached_message.text or cached_message.caption or ""
        source_entities = cached_message.entities or cached_message.caption_entities or []
        clean_text, clean_entities, buttons = _clean_text_entities(source_text, source_entities)
        rendered_note = {"text": clean_text, "entities": clean_entities, "buttons": buttons}
        if no_format:
            clean_text = _html_source(clean_text, [
                entity for item in clean_entities
                if (entity := _dict_to_entity(item)) is not None
            ])
            clean_entities = []
            rendered_note["text"] = clean_text
            rendered_note["entities"] = clean_entities
        entities = [] if no_format else [
            entity for item in clean_entities if (entity := _dict_to_entity(item)) is not None
        ]
        markup = _inline_keyboard(owner_id, rendered_note, audience_id)
        result = _cache_media_result(
            name,
            cached_message,
            owner_id,
            note,
            markup,
            clean_text,
            entities,
            no_format=no_format,
        )
        if result is None:
            if _has_media_payload(cached_message):
                rows = list(markup.inline_keyboard) if markup else []
                rows.append([InlineKeyboardButton(
                    "Buka pesan utuh",
                    callback_data=_note_callback_data("copy", owner_id, name, audience_id),
                )])
                markup = InlineKeyboardMarkup(rows)
            parse_mode, rendered_entities = _render_options(rendered_note, no_format=no_format)
            result = InlineQueryResultArticle(
                id=f"note_{owner_id}_{name}_{'n' if no_format else 'f'}",
                title=f"Catatan: {name}",
                description="Tampilkan catatan dengan tombol navigasi.",
                input_message_content=InputTextMessageContent(
                    clean_text or "Pesan ini tidak dapat ditampilkan sebagai teks inline.",
                    parse_mode=parse_mode,
                    entities=rendered_entities or None,
                    disable_web_page_preview=False,
                ),
                reply_markup=markup,
            )
    else:
        legacy_note = note if isinstance(note, dict) else {"text": note, "entities": [], "buttons": []}
        result = InlineQueryResultArticle(
            id=f"note_{owner_id}_{name}_{'n' if no_format else 'f'}",
            title=f"Catatan: {name}",
            description="Tampilkan catatan dengan tombol navigasi.",
            input_message_content=_message_content(legacy_note, no_format=no_format),
            reply_markup=_inline_keyboard(owner_id, legacy_note, audience_id),
        )
    return await inline_query.answer([result], cache_time=0, is_personal=True)


@bot.on_callback_query(
    filters.regex(
        r"^(?:note:(?:show|pop|copy):\d+:[\w-]{1,32}|"
        r"ns:[spc]:[0-9a-z]+:[\w-]{1,32}:[0-9a-z]+:[A-Za-z0-9_-]{8})$"
    ),
    group=-2,
)
async def note_callback(client, callback_query):
    if not callback_query.from_user:
        return await callback_query.answer("Pengguna tidak dikenali.", show_alert=True)
    if callback_query.data.startswith("ns:"):
        parsed = _parse_shared_note_callback(callback_query.data)
        if not parsed:
            return await callback_query.answer("Tombol catatan tidak valid.", show_alert=True)
        action, owner_id, name, audience_id = parsed
        if audience_id and callback_query.from_user.id != audience_id:
            return await callback_query.answer("Tombol ini ditujukan ke pengguna lain.", show_alert=True)
        if audience_id not in (0, owner_id) and not await is_sudo_user(owner_id, audience_id):
            return await callback_query.answer("Akses Sudo sudah dicabut.", show_alert=True)
    else:
        try:
            _, action, owner_text, name = callback_query.data.split(":", 3)
            owner_id = int(owner_text)
        except (AttributeError, TypeError, ValueError):
            return await callback_query.answer("Tombol catatan tidak valid.", show_alert=True)
        # Older callbacks were owner-scoped. Keep them usable as public buttons
        # when the note has already been posted into a shared chat.
        audience_id = 0
    if audience_id != owner_id and action == "copy":
        # A shared inline result is editable by the inline bot but the bot may
        # not be allowed to start a new private chat with the recipient.
        action = "show"
    note, cached_message = await _get_cached_note(owner_id, name.lower())
    if isinstance(note, str):
        note = {"text": note, "entities": [], "buttons": []}
    if not note:
        return await callback_query.answer("Catatan ini sudah dihapus.", show_alert=True)
    if note.get("cache_message_id") and not cached_message:
        return await callback_query.answer("Pesan cache tidak bisa dibaca oleh bot.", show_alert=True)
    if action == "copy" and cached_message:
        try:
            clean_text, entity_data, buttons = _clean_text_entities(
                cached_message.text or cached_message.caption or "",
                cached_message.entities or cached_message.caption_entities or [],
            )
            entities = [
                entity for item in entity_data if (entity := _dict_to_entity(item)) is not None
            ]
            await client.copy_message(
                chat_id=audience_id,
                from_chat_id=NOTES_CACHE_CHANNEL,
                message_id=int(note["cache_message_id"]),
                caption=clean_text if cached_message.caption else None,
                caption_entities=entities or None,
                parse_mode=ParseMode.DISABLED,
                reply_markup=_inline_keyboard(
                    owner_id, {"buttons": buttons}, audience_id
                ),
            )
            return await callback_query.answer("Pesan utuh dikirim.")
        except Exception as exc:
            print(f"[NOTES COPY] gagal mengirim pesan cache: {type(exc).__name__}")
            return await callback_query.answer("Pesan cache gagal dikirim.", show_alert=True)
    if action == "pop" or note.get("mode") == "popup":
        popup_text = (
            (cached_message.text or cached_message.caption or "")
            if cached_message
            else note.get("text", "")
        )
        return await callback_query.answer(popup_text[:180] or "", show_alert=True)
    try:
        if cached_message:
            text, entity_data, buttons = _clean_text_entities(
                cached_message.text or cached_message.caption or "",
                cached_message.entities or cached_message.caption_entities or [],
            )
            html_text = _as_html(text, entity_data)
            markup = _inline_keyboard(owner_id, {"buttons": buttons}, audience_id)
            media = _input_media(cached_message, html_text, [])
            if media:
                try:
                    await callback_query.edit_message_media(media=media, reply_markup=markup)
                    return await callback_query.answer()
                except Exception as exc:
                    if type(exc).__name__ == "MessageNotModified":
                        return await callback_query.answer()
            if text:
                try:
                    await callback_query.edit_message_text(
                        html_text,
                        parse_mode=ParseMode.HTML,
                        reply_markup=markup,
                        disable_web_page_preview=False,
                    )
                    return await callback_query.answer()
                except Exception as exc:
                    if type(exc).__name__ == "MessageNotModified":
                        return await callback_query.answer()
                    try:
                        await callback_query.edit_message_caption(
                            html_text,
                            parse_mode=ParseMode.HTML,
                            reply_markup=markup,
                        )
                        return await callback_query.answer()
                    except Exception as exc:
                        if type(exc).__name__ == "MessageNotModified":
                            return await callback_query.answer()
                        print(
                            f"[NOTES CALLBACK] edit text/caption gagal: "
                            f"{type(exc).__name__}: {str(exc)[:160]}"
                        )
                        return await callback_query.answer(
                            "Catatan gagal diperbarui. Coba tekan tombol lagi.",
                            show_alert=True,
                        )
            return await callback_query.answer(
                "Tidak bisa mengedit pesan inline ini. Gunakan tombol Buka pesan utuh.",
                show_alert=True,
            )

        html_text = _as_html(note.get("text", ""), note.get("entities", []))
        await callback_query.edit_message_text(
            html_text,
            parse_mode=ParseMode.HTML,
            reply_markup=_inline_keyboard(owner_id, note, audience_id),
            disable_web_page_preview=False,
        )
        return await callback_query.answer()
    except Exception as exc:
        print(f"[NOTES CALLBACK] gagal mengganti catatan: {type(exc).__name__}")
        return await callback_query.answer("Catatan gagal dibuka.", show_alert=True)


