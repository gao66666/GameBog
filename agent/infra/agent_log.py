"""
Agent 日志模块。
输出到 Go 项目 logger/agent_log/ 目录，按天切割，保留 7 天。
"""
import logging
import logging.handlers
import os
import sys
from datetime import datetime

_LOG_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "logger", "agent_log"))
_LOG_FORMAT = "%(asctime)s [%(levelname)s] %(name)s: %(message)s"
_DATE_FORMAT = "%Y-%m-%d %H:%M:%S"

_loggers: dict[str, logging.Logger] = {}


def _ensure_dir(path: str):
    if not os.path.exists(path):
        os.makedirs(path, exist_ok=True)


def get_logger(name: str = "agent") -> logging.Logger:
    """获取命名 logger。同一个 name 返回同一实例，避免重复 handler。"""
    if name in _loggers:
        return _loggers[name]

    _ensure_dir(_LOG_DIR)
    logger = logging.getLogger(name)

    # 防止重复添加 handler
    if logger.handlers:
        return logger

    logger.setLevel(logging.DEBUG)
    logger.propagate = False

    # 文件 handler — 按天切割，保留 7 天
    today = datetime.now().strftime("%Y%m%d")
    log_file = os.path.join(_LOG_DIR, f"{today}.log")
    file_handler = logging.handlers.TimedRotatingFileHandler(
        log_file, when="midnight", interval=1, backupCount=7, encoding="utf-8"
    )
    file_handler.setLevel(logging.DEBUG)

    # 控制台 handler
    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setLevel(logging.INFO)

    formatter = logging.Formatter(_LOG_FORMAT, _DATE_FORMAT)
    file_handler.setFormatter(formatter)
    console_handler.setFormatter(formatter)

    logger.addHandler(file_handler)
    logger.addHandler(console_handler)

    _loggers[name] = logger
    return logger
