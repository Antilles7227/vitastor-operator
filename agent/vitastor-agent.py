from __future__ import annotations
from typing import Tuple, Optional
import logging
import os
import sys
import asyncio
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
import json
import re

class Partition(BaseModel):
    partuuid: Optional[str]
    name: str
    fstype: Optional[str]


class Disk(BaseModel):
    name: str
    type: str
    children: Optional[list[Partition]]


class LsblkOutput(BaseModel):
    blockdevices: list[Disk]


class VitastorParameters(BaseModel):
    data_device: str
    bitmap_granularity: int
    block_size: int
    osd_num: int
    disable_data_fsync: bool
    disable_device_lock: bool
    immediate_commit: str
    disk_alignment: int
    journal_block_size: int
    meta_block_size: int
    journal_no_same_sector_overwrites: bool
    journal_sector_buffer_count: int

class OSDPrepareParameters(BaseModel):
    disk: str
    osd_num: Optional[int]


app = FastAPI()


async def exec_shell(script: str) -> Tuple[int, str, str]:
    proc = await asyncio.create_subprocess_shell(script, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
    stdout, stderr = await proc.communicate()
    if stdout:
        res_stdout = stdout.decode()
    if stderr:
        res_stderr = stderr.decode()
    return proc.returncode, res_stdout, res_stderr

async def get_system_disks(disk_mask: str = "") -> list[Disk]:
    system_disks: list[Disk] = []
    result: LsblkOutput
    proc = await asyncio.create_subprocess_shell(f"lsblk -p -J -o NAME,PARTUUID,FSTYPE,TYPE {disk_mask}",
                                                stdout=asyncio.subprocess.PIPE,
                                                stderr=asyncio.subprocess.PIPE)
    stdout, stderr = await proc.communicate()
    if stdout:
        print(stdout.decode())
        result = LsblkOutput.parse_raw(stdout.decode())
    for block in  result.blockdevices:
        if block.type != "disk":
            continue
        system_disks.append(block)
    return system_disks

@app.on_event('startup')
async def app_startup():
    pass


@app.on_event('shutdown')
async def app_shutdown():
    pass

@app.get("/disk")
async def get_disks() -> list[Disk]:
    result = await get_system_disks()
    return result


@app.get("/disk/empty")
async def get_empty_disks() -> list[Disk]:
    all_disks = await get_system_disks()
    empty_disks: list[Disk] = []
    for disk in all_disks:
        if not disk.children and (not "nbd" in disk.name):
            empty_disks.append(disk)
    return empty_disks

@app.get("/disk/osd")
async def get_osd_disks() -> Optional[list[VitastorParameters]]:
    osd_info: list[VitastorParameters] = []
    osd_list = os.listdir("/dev/disk/by-partuuid")
    if osd_list == 0:
        return None
    for osd in osd_list:
        osd_info_script = f"vitastor-disk read-sb '/dev/disk/by-partuuid/{osd}'"
        print(osd_info_script)
        osd_info_proc = await asyncio.create_subprocess_shell(osd_info_script, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
        stdout, stderr = await osd_info_proc.communicate()
        if osd_info_proc.returncode != 0:
            logging.error(f"Error during getting info, looks like it's not OSD partition, error: {stderr.decode()}")
            continue
        print(f"OSD info: {stdout.decode()}")
        osd_info.append(VitastorParameters.parse_raw(stdout.decode()))
    return osd_info


@app.post("/disk/prepare")
async def prepare_disk_for_vitastor(device: OSDPrepareParameters) -> Optional[list[VitastorParameters]]:
    aligned_disks = await get_system_disks(device.disk)
    if aligned_disks[0].children is not None:
        raise HTTPException(status_code=409, detail="That disk already partitioned and can't be prepared automatically. Please check it manually")
    vitastor_parameters: list[VitastorParameters] = []
    vitastor_prepare_script = f"vitastor-disk prepare {device.disk}"
    if device.osd_num:
        vitastor_prepare_script = f"vitastor-disk prepare {device.disk} --osd_per_disk {device.osd_num}"
    
    vitastor_prepare_proc = await asyncio.create_subprocess_shell(vitastor_prepare_script, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
    stdout, stderr = await vitastor_prepare_proc.communicate()
    if vitastor_prepare_proc.returncode != 0:
        logging.error("Something happen during preparing disk for OSD")
        raise HTTPException(status_code=500, detail="Error during preparing")
    disk_list = await get_system_disks(device.disk)
    print(disk_list)
    for d in disk_list[0].children:
        print(d)
        print(d.name)
        osd_info_script = f"vitastor-disk read-sb {d.name}"
        osd_info_proc = await asyncio.create_subprocess_shell(osd_info_script, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
        stdout, stderr = await osd_info_proc.communicate()
        if osd_info_proc.returncode != 0:
            logging.error("Something happen during gathering OSD info")
            logging.error(stderr.decode())
            continue
        vitastor_parameters.append(VitastorParameters.parse_raw(stdout.decode()))
    return vitastor_parameters